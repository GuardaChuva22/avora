// Package whatsapp links the owner's WhatsApp to the Avora server as a
// device (like WhatsApp Web), with whatsmeow. The device keys live in the
// server's Postgres (whatsmeow_* tables); chats and messages are kept in
// wa_chats and wa_messages so agents can read them. Only the server talks
// to WhatsApp; nothing runs on the owner's PC.
package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/GuardaChuva22/avora/internal/control"
)

const schema = `
CREATE TABLE IF NOT EXISTS wa_chats (
    jid       text PRIMARY KEY,
    name      text NOT NULL DEFAULT '',
    phone     text NOT NULL DEFAULT '',
    is_group  boolean NOT NULL DEFAULT false,
    last_at   timestamptz NOT NULL,
    last_text text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS wa_messages (
    chat_jid  text NOT NULL,
    id        text NOT NULL,
    sender    text NOT NULL DEFAULT '',
    from_me   boolean NOT NULL DEFAULT false,
    text      text NOT NULL,
    at        timestamptz NOT NULL,
    PRIMARY KEY (chat_jid, id)
);
CREATE INDEX IF NOT EXISTS wa_messages_chat_at ON wa_messages (chat_jid, at DESC);
ALTER TABLE wa_messages ADD COLUMN IF NOT EXISTS media_kind text NOT NULL DEFAULT '';
ALTER TABLE wa_messages ADD COLUMN IF NOT EXISTS media bytea;
CREATE INDEX IF NOT EXISTS wa_chats_last ON wa_chats (last_at DESC);`

// Service is the linked WhatsApp account.
type Service struct {
	db        *sql.DB
	container *sqlstore.Container

	mu        sync.Mutex
	cli       *whatsmeow.Client
	qr        string
	pairing   bool
	lastErr   string
	onMessage func(control.WAMessage)
	sent      map[string]time.Time // ids this server sent, so its own answers do not loop back
	names     map[string]string    // group names already fetched
}

// New opens the store (creating the tables) and reconnects a linked device.
func New(ctx context.Context, db *sql.DB) (*Service, error) {
	container := sqlstore.NewWithDB(db, "postgres", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("whatsapp store: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("whatsapp tables: %w", err)
	}
	s := &Service{db: db, container: container, sent: map[string]time.Time{}, names: map[string]string{}}
	dev, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, err
	}
	if dev.ID != nil {
		s.start(dev)
		go func() {
			if err := s.cli.Connect(); err != nil {
				s.setErr("não consegui conectar: " + err.Error())
			}
		}()
	}
	return s, nil
}

func (s *Service) setErr(e string) {
	s.mu.Lock()
	s.lastErr = e
	s.mu.Unlock()
	if e != "" {
		log.Printf("whatsapp: %s", e)
	}
}

// start makes the client for a device and listens to its events.
func (s *Service) start(dev *store.Device) {
	cli := whatsmeow.NewClient(dev, waLog.Noop)
	cli.EnableAutoReconnect = true
	cli.AddEventHandler(s.handle)
	s.mu.Lock()
	s.cli = cli
	s.mu.Unlock()
}

func (s *Service) client() *whatsmeow.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cli
}

// OnMessage sets who hears new messages.
func (s *Service) OnMessage(fn func(control.WAMessage)) {
	s.mu.Lock()
	s.onMessage = fn
	s.mu.Unlock()
}

// Status says whether a device is linked and online.
func (s *Service) Status() control.WAStatus {
	s.mu.Lock()
	cli, st := s.cli, control.WAStatus{Pairing: s.pairing, Error: s.lastErr}
	s.mu.Unlock()
	if cli != nil && cli.Store.ID != nil {
		st.Linked = true
		st.Online = cli.IsConnected() && cli.IsLoggedIn()
		st.Phone = "+" + cli.Store.ID.User
		st.Name = cli.Store.PushName
	}
	return st
}

// Connect starts pairing (a QR code to scan) or reconnects a linked device.
func (s *Service) Connect(ctx context.Context) error {
	if cli := s.client(); cli != nil && cli.Store.ID != nil {
		if !cli.IsConnected() {
			return cli.Connect()
		}
		return nil
	}
	s.mu.Lock()
	if s.pairing {
		s.mu.Unlock()
		return nil
	}
	s.pairing, s.qr, s.lastErr = true, "", ""
	s.mu.Unlock()
	s.start(s.container.NewDevice())
	cli := s.client()
	qrs, err := cli.GetQRChannel(context.WithoutCancel(ctx))
	if err != nil {
		s.mu.Lock()
		s.pairing = false
		s.mu.Unlock()
		return err
	}
	if err := cli.Connect(); err != nil {
		s.mu.Lock()
		s.pairing = false
		s.mu.Unlock()
		return err
	}
	go func() {
		for item := range qrs {
			s.mu.Lock()
			switch item.Event {
			case "code":
				s.qr = item.Code
			case "success":
				s.qr, s.pairing = "", false
			default: // timeout or error: the owner starts again
				s.qr, s.pairing = "", false
				if item.Error != nil {
					s.lastErr = "pareamento falhou: " + item.Error.Error()
				} else if item.Event == "timeout" {
					s.lastErr = "o QR expirou; tente de novo"
				}
			}
			s.mu.Unlock()
		}
	}()
	return nil
}

// Logout unlinks the device (it leaves the phone's linked devices).
func (s *Service) Logout(ctx context.Context) error {
	cli := s.client()
	if cli == nil {
		return nil
	}
	var err error
	if cli.Store.ID != nil {
		err = cli.Logout(ctx)
		if err != nil { // offline: forget it here anyway
			_ = s.container.DeleteDevice(ctx, cli.Store)
		}
	}
	cli.Disconnect()
	s.mu.Lock()
	s.cli, s.qr, s.pairing, s.lastErr = nil, "", false, ""
	s.mu.Unlock()
	return nil
}

// QRPNG is the current pairing code as an image.
func (s *Service) QRPNG() ([]byte, error) {
	s.mu.Lock()
	code := s.qr
	s.mu.Unlock()
	if code == "" {
		return nil, errors.New("no qr code now")
	}
	return qrcode.Encode(code, qrcode.Medium, 512)
}

// ── Events ──────────────────────────────────────────────────────

func (s *Service) handle(evt any) {
	switch e := evt.(type) {
	case *events.Message:
		s.message(e, true)
	case *events.HistorySync:
		s.history(e)
	case *events.Connected:
		s.setErr("")
	case *events.LoggedOut:
		s.setErr("o WhatsApp desconectou este aparelho; conecte de novo")
		s.mu.Lock()
		s.cli = nil
		s.mu.Unlock()
	}
}

func (s *Service) history(e *events.HistorySync) {
	cli := s.client()
	if cli == nil {
		return
	}
	for _, conv := range e.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		if name := conv.GetName(); name != "" {
			s.mu.Lock()
			s.names[chat.String()] = name
			s.mu.Unlock()
		}
		for _, hm := range conv.GetMessages() {
			if msg, err := cli.ParseWebMessage(chat, hm.GetMessage()); err == nil {
				s.message(msg, false)
			}
		}
	}
}

// text is what a message says, with a tag for media.
func text(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	switch {
	case m.GetConversation() != "":
		return m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return strings.TrimSpace("[imagem] " + m.GetImageMessage().GetCaption())
	case m.GetVideoMessage() != nil:
		return strings.TrimSpace("[vídeo] " + m.GetVideoMessage().GetCaption())
	case m.GetAudioMessage() != nil:
		return "[áudio]"
	case m.GetDocumentMessage() != nil:
		return "[documento] " + m.GetDocumentMessage().GetFileName()
	case m.GetStickerMessage() != nil:
		return "[figurinha]"
	case m.GetLocationMessage() != nil:
		return "[localização]"
	case m.GetContactMessage() != nil:
		return "[contato] " + m.GetContactMessage().GetDisplayName()
	}
	return ""
}

// phoneJID turns a hidden id (@lid) into the phone number's JID when known.
func (s *Service) phoneJID(ctx context.Context, cli *whatsmeow.Client, j types.JID) types.JID {
	j = j.ToNonAD()
	if j.Server == types.HiddenUserServer {
		if pn, err := cli.Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD()
		}
	}
	return j
}

func (s *Service) message(e *events.Message, live bool) {
	cli := s.client()
	if cli == nil || e.Info.Chat.Server == types.BroadcastServer || e.Info.Chat.Server == types.NewsletterServer {
		return
	}
	body := strings.TrimSpace(text(e.Message))
	if body == "" {
		return
	}
	ctx := context.Background()
	chat := s.phoneJID(ctx, cli, e.Info.Chat)
	sender := s.phoneJID(ctx, cli, e.Info.Sender)
	own := cli.Store.ID
	self := own != nil && !e.Info.IsGroup && chat.User == own.User
	if !self && own != nil && cli.Store.LID.User != "" && chat.User == cli.Store.LID.User {
		self = true
	}
	name := s.chatName(ctx, cli, chat, e.Info.IsGroup, e.Info.PushName, e.Info.IsFromMe)
	senderName := e.Info.PushName
	if e.Info.IsFromMe {
		senderName = "Dono"
	} else if senderName == "" {
		senderName = s.contactName(ctx, cli, sender)
	}
	phone := ""
	if !e.Info.IsGroup && chat.Server == types.DefaultUserServer {
		phone = "+" + chat.User
	} else if sender.Server == types.DefaultUserServer && !e.Info.IsFromMe {
		phone = "+" + sender.User
	}
	at := e.Info.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	kind, media := mediaOf(e.Message)
	_, _ = s.db.ExecContext(ctx, `INSERT INTO wa_messages (chat_jid, id, sender, from_me, text, at, media_kind, media) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (chat_jid, id) DO NOTHING`, chat.String(), e.Info.ID, senderName, e.Info.IsFromMe, body, at, kind, media)
	chatPhone := ""
	if !e.Info.IsGroup && chat.Server == types.DefaultUserServer {
		chatPhone = "+" + chat.User
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO wa_chats (jid, name, phone, is_group, last_at, last_text) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (jid) DO UPDATE SET name = CASE WHEN EXCLUDED.name <> '' THEN EXCLUDED.name ELSE wa_chats.name END,
			phone = CASE WHEN EXCLUDED.phone <> '' THEN EXCLUDED.phone ELSE wa_chats.phone END,
			last_at = GREATEST(wa_chats.last_at, EXCLUDED.last_at),
			last_text = CASE WHEN EXCLUDED.last_at >= wa_chats.last_at THEN EXCLUDED.last_text ELSE wa_chats.last_text END`,
		chat.String(), name, chatPhone, e.Info.IsGroup, at, truncate(body, 300))
	if !live || time.Since(at) > 10*time.Minute {
		return
	}
	s.mu.Lock()
	_, ours := s.sent[e.Info.ID]
	fn := s.onMessage
	s.mu.Unlock()
	if ours || fn == nil {
		return
	}
	fn(control.WAMessage{ID: e.Info.ID, ChatJID: chat.String(), ChatName: name, Phone: phone, Sender: senderName,
		FromMe: e.Info.IsFromMe, Self: self, Text: body, At: at, Media: kind})
}

// mediaOf is the kind of media a message carries and the message itself,
// kept so the file can be downloaded later (WhatsApp keeps it for weeks).
func mediaOf(m *waE2E.Message) (string, []byte) {
	if m == nil {
		return "", nil
	}
	kind := ""
	switch {
	case m.GetImageMessage() != nil:
		kind = "image"
	case m.GetAudioMessage() != nil:
		kind = "audio"
	case m.GetVideoMessage() != nil:
		kind = "video"
	case m.GetDocumentMessage() != nil:
		kind = "document"
	case m.GetStickerMessage() != nil:
		kind = "sticker"
	default:
		return "", nil
	}
	raw, err := proto.Marshal(m)
	if err != nil {
		return "", nil
	}
	return kind, raw
}

// Media downloads the file of a message.
func (s *Service) Media(ctx context.Context, chat, id string) (control.WAMedia, error) {
	cli := s.client()
	if cli == nil {
		return control.WAMedia{}, errors.New("o WhatsApp não está conectado")
	}
	var kind string
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT media_kind, media FROM wa_messages WHERE chat_jid = $1 AND id = $2`, chat, id).Scan(&kind, &raw)
	if err != nil || kind == "" || len(raw) == 0 {
		return control.WAMedia{}, errors.New("essa mensagem não tem mídia guardada")
	}
	m := &waE2E.Message{}
	if err := proto.Unmarshal(raw, m); err != nil {
		return control.WAMedia{}, err
	}
	var dm whatsmeow.DownloadableMessage
	out := control.WAMedia{Kind: kind}
	switch kind {
	case "image":
		x := m.GetImageMessage()
		dm, out.Mime, out.Caption, out.Size = x, x.GetMimetype(), x.GetCaption(), x.GetFileLength()
	case "audio":
		x := m.GetAudioMessage()
		dm, out.Mime, out.Size = x, x.GetMimetype(), x.GetFileLength()
	case "video":
		x := m.GetVideoMessage()
		dm, out.Mime, out.Caption, out.Size = x, x.GetMimetype(), x.GetCaption(), x.GetFileLength()
	case "document":
		x := m.GetDocumentMessage()
		dm, out.Mime, out.Name, out.Caption, out.Size = x, x.GetMimetype(), x.GetFileName(), x.GetCaption(), x.GetFileLength()
	case "sticker":
		x := m.GetStickerMessage()
		dm, out.Mime, out.Size = x, x.GetMimetype(), x.GetFileLength()
	}
	if out.Size > control.MaxWAMedia {
		return out, fmt.Errorf("arquivo grande demais (%d MB); o limite é %d MB", out.Size>>20, control.MaxWAMedia>>20)
	}
	data, err := cli.Download(ctx, dm)
	if err != nil {
		return out, fmt.Errorf("não consegui baixar a mídia: %v", err)
	}
	out.Data = data
	out.Mime = strings.TrimSpace(strings.SplitN(out.Mime, ";", 2)[0])
	return out, nil
}

func (s *Service) contactName(ctx context.Context, cli *whatsmeow.Client, j types.JID) string {
	if c, err := cli.Store.Contacts.GetContact(ctx, j); err == nil {
		for _, n := range []string{c.FullName, c.FirstName, c.BusinessName, c.PushName} {
			if n != "" {
				return n
			}
		}
	}
	return ""
}

func (s *Service) chatName(ctx context.Context, cli *whatsmeow.Client, chat types.JID, group bool, pushName string, fromMe bool) string {
	s.mu.Lock()
	known := s.names[chat.String()]
	s.mu.Unlock()
	if known != "" {
		return known
	}
	name := ""
	if group {
		if info, err := cli.GetGroupInfo(ctx, chat); err == nil {
			name = info.Name
		}
	} else {
		name = s.contactName(ctx, cli, chat)
		if name == "" && !fromMe {
			name = pushName
		}
	}
	if name != "" {
		s.mu.Lock()
		s.names[chat.String()] = name
		s.mu.Unlock()
	}
	return name
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// ── Reading ─────────────────────────────────────────────────────

func display(c control.WAChat) control.WAChat {
	if c.Name == "" {
		c.Name = orElse(c.Phone, c.JID)
	}
	return c
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Chats lists recent chats, or those matching query by name, number or text.
func (s *Service) Chats(ctx context.Context, query string, limit int) ([]control.WAChat, error) {
	q := "%" + strings.ToLower(query) + "%"
	digits := "%" + nonDigits.ReplaceAllString(query, "") + "%"
	if nonDigits.ReplaceAllString(query, "") == "" {
		digits = "\x00"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT jid, name, phone, is_group, last_at, last_text FROM wa_chats c
		WHERE $1 = '%%' OR lower(name) LIKE $1 OR phone LIKE $2
			OR EXISTS (SELECT 1 FROM wa_messages m WHERE m.chat_jid = c.jid AND lower(m.text) LIKE $1)
		ORDER BY last_at DESC LIMIT $3`, q, digits, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []control.WAChat{}
	for rows.Next() {
		var c control.WAChat
		if err := rows.Scan(&c.JID, &c.Name, &c.Phone, &c.Group, &c.LastAt, &c.LastText); err != nil {
			return nil, err
		}
		out = append(out, display(c))
	}
	return out, rows.Err()
}

// Messages are a chat's last messages, oldest first.
func (s *Service) Messages(ctx context.Context, chat string, limit int) ([]control.WAMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, sender, from_me, text, at, media_kind FROM
		(SELECT * FROM wa_messages WHERE chat_jid = $1 ORDER BY at DESC LIMIT $2) last ORDER BY at`, chat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []control.WAMessage{}
	for rows.Next() {
		m := control.WAMessage{ChatJID: chat}
		if err := rows.Scan(&m.ID, &m.Sender, &m.FromMe, &m.Text, &m.At, &m.Media); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

var nonDigits = regexp.MustCompile(`\D+`)

// Resolve finds a chat by JID, phone number or name.
func (s *Service) Resolve(ctx context.Context, to string) (control.WAChat, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return control.WAChat{}, errors.New("diga para quem")
	}
	if strings.Contains(to, "@") {
		j, err := types.ParseJID(to)
		if err != nil {
			return control.WAChat{}, errors.New("jid inválido")
		}
		var c control.WAChat
		err = s.db.QueryRowContext(ctx, `SELECT jid, name, phone, is_group, last_at, last_text FROM wa_chats WHERE jid = $1`, j.String()).
			Scan(&c.JID, &c.Name, &c.Phone, &c.Group, &c.LastAt, &c.LastText)
		if err != nil {
			c = control.WAChat{JID: j.String()}
			if j.Server == types.DefaultUserServer {
				c.Phone = "+" + j.User
			}
		}
		return display(c), nil
	}
	if d := nonDigits.ReplaceAllString(to, ""); len(d) >= 8 && len(d) >= len(strings.TrimSpace(to))/2 {
		return s.resolvePhone(ctx, d)
	}
	chats, err := s.Chats(ctx, to, 6)
	if err != nil {
		return control.WAChat{}, err
	}
	var exact []control.WAChat
	for _, c := range chats {
		if strings.EqualFold(c.Name, to) {
			exact = append(exact, c)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	var byName []control.WAChat
	for _, c := range chats {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(to)) {
			byName = append(byName, c)
		}
	}
	switch len(byName) {
	case 1:
		return byName[0], nil
	case 0:
		return control.WAChat{}, fmt.Errorf("não achei conversa com %q; passe o número com DDD", to)
	}
	names := []string{}
	for _, c := range byName {
		names = append(names, c.Name+" ("+orElse(c.Phone, c.JID)+")")
	}
	return control.WAChat{}, fmt.Errorf("mais de uma conversa com %q: %s; use o número ou o jid", to, strings.Join(names, "; "))
}

// resolvePhone checks a number on WhatsApp (Brazilian numbers may be
// written without 55 and with or without the extra 9).
func (s *Service) resolvePhone(ctx context.Context, d string) (control.WAChat, error) {
	if len(d) == 10 || len(d) == 11 {
		d = "55" + d
	}
	cli := s.client()
	if cli == nil || !cli.IsLoggedIn() {
		return control.WAChat{}, errors.New("o WhatsApp não está conectado")
	}
	res, err := cli.IsOnWhatsApp(ctx, []string{"+" + d})
	if err != nil {
		return control.WAChat{}, fmt.Errorf("não consegui conferir o número: %w", err)
	}
	for _, r := range res {
		if r.IsIn {
			j := r.JID.ToNonAD()
			c := control.WAChat{JID: j.String(), Phone: "+" + j.User}
			_ = s.db.QueryRowContext(ctx, `SELECT name FROM wa_chats WHERE jid = $1`, j.String()).Scan(&c.Name)
			return display(c), nil
		}
	}
	return control.WAChat{}, fmt.Errorf("o número +%s não tem WhatsApp", d)
}

// Send sends a text message.
func (s *Service) Send(ctx context.Context, jid, body string) error {
	cli := s.client()
	if cli == nil || !cli.IsLoggedIn() {
		return errors.New("o WhatsApp não está conectado")
	}
	to, err := types.ParseJID(jid)
	if err != nil {
		return errors.New("destino inválido")
	}
	id := cli.GenerateMessageID()
	s.mu.Lock()
	s.sent[id] = time.Now()
	for k, t := range s.sent {
		if time.Since(t) > time.Hour {
			delete(s.sent, k)
		}
	}
	s.mu.Unlock()
	resp, err := cli.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(body)}, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		return fmt.Errorf("o WhatsApp recusou: %w", err)
	}
	at := resp.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO wa_messages (chat_jid, id, sender, from_me, text, at) VALUES ($1, $2, 'Dono', true, $3, $4)
		ON CONFLICT (chat_jid, id) DO NOTHING`, to.String(), id, body, at)
	_, _ = s.db.ExecContext(ctx, `UPDATE wa_chats SET last_at = $2, last_text = $3 WHERE jid = $1`, to.String(), at, truncate(body, 300))
	return nil
}
