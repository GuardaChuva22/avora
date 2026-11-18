import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Create or edit a routine: a prompt one agent runs on a schedule.
Sheet {
    id: ed
    property var agent: null
    property var routine: null // null = new
    property string error: ""
    property bool busy: false
    signal saved

    property string mode: "at"     // "at", "every" or "event"
    // An event trigger (mode "event").
    property string trigKind: "email" // "email", "clickup", "no"
    property string trigNode: ""
    property string trigEvent: "offline"
    property int trigEvery: 5
    property var nodes: []
    property var days: []          // 0 = Sunday
    property string date: ""       // a reminder: runs once that day (kept until days are picked)
    property int every: 60
    property string deliver: "time"
    property string resp: ""
    property string skill: ""
    property var skills: [] // the ones this agent can see

    readonly property var dayNames: ["D", "S", "T", "Q", "Q", "S", "S"]
    readonly property var dayFull: ["domingo", "segunda", "terça", "quarta", "quinta", "sexta", "sábado"]

    function open(a, r, preset) {
        agent = a;
        routine = r;
        error = "";
        busy = false;
        const p = r || preset || {};
        title.text = p.title || "";
        prompt.text = p.prompt || "";
        const sc = p.schedule && (p.schedule.at || p.schedule.every) ? p.schedule : { at: "08:00", days: [1, 2, 3, 4, 5] };
        mode = p.trigger ? "event" : sc.every ? "every" : "at";
        const tr = p.trigger || {};
        const targs = tr.args || {};
        trigKind = tr.kind === "no" ? "no" : tr.tool === "clickup_tarefas" ? "clickup" : "email";
        trigSearch.text = targs.busca || (tr.kind ? "" : "in:inbox");
        trigList.text = targs.lista_id || "";
        trigNode = tr.node || "";
        trigEvent = tr.event || "offline";
        trigEvery = tr.every || 5;
        Api.get("/v1/nodes", (err, d) => {
            if (!err) {
                ed.nodes = d.filter(n => !n.revoked_at);
                if (ed.trigNode === "" && ed.nodes.length)
                    ed.trigNode = ed.nodes[0].id;
            }
        });
        at.text = sc.at || "08:00";
        days = (sc.days || []).slice();
        date = sc.date || "";
        every = sc.every || 60;
        deliver = p.deliver || "time";
        resp = p.responsibility_id || "";
        skill = p.skill || "";
        skills = [];
        Api.get("/v1/skills", (err, data) => {
            if (!err)
                skills = data.filter(s => !s.project_id || !a.project_id || s.project_id === a.project_id);
        });
        shown = true;
        title.forceActiveFocus();
    }

    function toggleDay(d) {
        const i = days.indexOf(d);
        const next = days.slice();
        if (i >= 0)
            next.splice(i, 1);
        else
            next.push(d);
        next.sort();
        days = next;
    }

    function trigger() {
        if (trigKind === "no")
            return { kind: "no", node: trigNode, event: trigEvent };
        if (trigKind === "clickup")
            return { kind: "conector", tool: "clickup_tarefas", every: trigEvery, args: { busca: trigSearch.text.trim(), lista_id: trigList.text.trim() } };
        return { kind: "conector", tool: "gmail_buscar", every: trigEvery, args: { busca: trigSearch.text.trim() || "in:inbox" } };
    }

    function body() {
        if (mode === "event")
            return { agent_id: agent.id, title: title.text.trim(), prompt: prompt.text.trim(), schedule: {}, trigger: trigger(),
                     deliver: deliver, responsibility_id: resp, skill: skill, enabled: routine ? routine.enabled : true };
        const sc = mode === "every" ? { every: every }
                 : date !== "" && days.length === 0 ? { at: at.text.trim(), date: date }
                 : { at: at.text.trim(), days: days.length === 7 ? [] : days };
        return { agent_id: agent.id, title: title.text.trim(), prompt: prompt.text.trim(), schedule: sc, deliver: deliver,
                 responsibility_id: resp, skill: skill, enabled: routine ? routine.enabled : true };
    }

    function done(err) {
        busy = false;
        if (err) {
            error = err;
            return;
        }
        shown = false;
        saved();
    }

    function save(then) {
        if (busy)
            return;
        if (title.text.trim() === "" || prompt.text.trim() === "") {
            error = "Dê um nome e diga o que fazer.";
            return;
        }
        if (mode === "at" && !/^([01]\d|2[0-3]):[0-5]\d$/.test(at.text.trim())) {
            error = "Horário no formato 08:00.";
            return;
        }
        busy = true;
        const cb = (err, r) => {
            if (err || !then)
                return done(err);
            then(r);
        };
        if (routine)
            Api.put("/v1/routines/" + routine.id, body(), cb);
        else
            Api.post("/v1/routines", body(), cb);
    }

    function runNow() {
        save(r => Api.post("/v1/routines/" + r.id + "/run", null, err => done(err)));
    }

    function remove() {
        if (!routine || busy)
            return;
        busy = true;
        Api.del("/v1/routines/" + routine.id, err => done(err));
    }




    Row {
        spacing: 14
        Avatar {
            size: 46
            name: ed.agent ? ed.agent.name : "?"
            photo: U.photo(ed.agent)
            tint: ed.agent && ed.agent.color ? ed.agent.color : Theme.text2
            anchors.verticalCenter: parent.verticalCenter
        }
        Column {
            anchors.verticalCenter: parent.verticalCenter
            spacing: 3
            Text {
                text: ed.routine ? "Editar rotina" : "Nova rotina"
                color: Theme.white
                font.family: Theme.display
                font.pixelSize: Theme.fontHeading
                font.weight: Font.DemiBold
            }
            Text {
                text: ed.agent ? ed.agent.name + " roda sozinho no horário e entrega o resultado." : ""
                color: Theme.text3
                font.pixelSize: Theme.fontBody
            }
        }
    }

    Column {
        width: parent.width
        spacing: 8
        Overline { text: "NOME" }
        Field { id: title; width: parent.width; placeholderText: "Ex.: Checar gastos do mês"; maximumLength: 80 }
    }
    Column {
        width: parent.width
        spacing: 8
        Overline { text: "O QUE FAZER" }
        AreaField {
            id: prompt
            width: parent.width
            minHeight: 110
            maxHeight: 220
            enterSubmits: false
            placeholderText: "O pedido que ele recebe a cada vez. Ele também recebe o estado do Avora: nós, tarefas das últimas 24 h, rotinas e responsabilidades."
        }
    }

    Column {
        width: parent.width
        spacing: 10
        Overline { text: "QUANDO" }
        Segmented {
            value: ed.mode
            options: [{ value: "at", label: "Em um horário", icon: "history" }, { value: "every", label: "A cada intervalo", icon: "refresh" },
                      { value: "event", label: "Quando algo acontecer", icon: "bolt" }]
            onActivated: v => ed.mode = v
        }
        // An event: new e-mail, new ClickUp task, a node going on/off.
        Column {
            visible: ed.mode === "event"
            width: parent.width
            spacing: 10
            Segmented {
                value: ed.trigKind
                options: [{ value: "email", label: "E-mail novo", icon: "mail" }, { value: "clickup", label: "Tarefa no ClickUp", icon: "tasks" },
                          { value: "no", label: "PC ou VPS", icon: "server" }]
                onActivated: v => ed.trigKind = v
            }
            Row {
                visible: ed.trigKind !== "no"
                spacing: 10
                Field {
                    id: trigSearch
                    width: ed.trigKind === "clickup" ? 260 : 400
                    placeholderText: ed.trigKind === "clickup" ? "Texto no nome (opcional)" : "Busca do Gmail, ex.: from:fornecedor is:unread"
                }
                Field {
                    id: trigList
                    visible: ed.trigKind === "clickup"
                    width: 150
                    mono: true
                    placeholderText: "Id da lista"
                }
            }
            Segmented {
                visible: ed.trigKind !== "no"
                value: "" + ed.trigEvery
                options: [{ value: "2", label: "2 min" }, { value: "5", label: "5 min" }, { value: "15", label: "15 min" },
                          { value: "60", label: "1 h" }]
                onActivated: v => ed.trigEvery = parseInt(v)
            }
            Row {
                visible: ed.trigKind === "no"
                spacing: 10
                Segmented {
                    value: ed.trigNode
                    options: ed.nodes.map(n => ({ value: n.id, label: n.name, icon: n.kind === "vps" ? "server" : "monitor" }))
                    onActivated: v => ed.trigNode = v
                }
                Segmented {
                    value: ed.trigEvent
                    options: [{ value: "offline", label: "Desligar" }, { value: "online", label: "Ligar" }]
                    onActivated: v => ed.trigEvent = v
                }
            }
        }
        Row {
            visible: ed.mode === "at"
            spacing: 14
            Field {
                id: at
                width: 96
                mono: true
                horizontalAlignment: TextInput.AlignHCenter
                maximumLength: 5
                placeholderText: "08:00"
            }
            Row {
                anchors.verticalCenter: parent.verticalCenter
                spacing: 6
                Repeater {
                    model: 7
                    delegate: Rectangle {
                        id: dayChip
                        required property int index
                        // Monday first, as people read a week here.
                        readonly property int day: (index + 1) % 7
                        readonly property bool on: ed.days.indexOf(day) >= 0 || ed.days.length === 0
                        width: 34
                        height: 34
                        radius: Theme.radius
                        color: on ? Theme.white : dayArea.containsMouse ? Theme.hover : "transparent"
                        border.width: 1
                        border.color: on ? Theme.white : Theme.line
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                        Text {
                            anchors.centerIn: parent
                            text: ed.dayNames[dayChip.day]
                            color: dayChip.on ? Theme.ink : Theme.text3
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.DemiBold
                        }
                        MouseArea {
                            id: dayArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (ed.days.length === 0)
                                    ed.days = [0, 1, 2, 3, 4, 5, 6];
                                ed.toggleDay(dayChip.day);
                                if (ed.days.length === 0)
                                    ed.days = [dayChip.day]; // never "no day"
                            }
                        }
                    }
                }
            }
        }
        Segmented {
            visible: ed.mode === "every"
            value: "" + ed.every
            options: [{ value: "15", label: "15 min" }, { value: "30", label: "30 min" }, { value: "60", label: "1 h" },
                      { value: "120", label: "2 h" }, { value: "360", label: "6 h" }, { value: "720", label: "12 h" }]
            onActivated: v => ed.every = parseInt(v)
        }
        Text {
            width: ed.form.width
            wrapMode: Text.Wrap
            text: ed.mode === "event" ? (ed.trigKind === "no" ? "Roda " + U.triggerText({ kind: "no", node_name: (ed.nodes.find(n => n.id === ed.trigNode) || {}).name, event: ed.trigEvent }) + ". O agente recebe o que aconteceu."
                                          : "Confere a cada " + ed.trigEvery + " min e roda com o que chegou de novo (a primeira conferida só aprende o que já existe). Usa a conta do Plugin.")
                  : ed.mode === "every" ? "Roda " + U.scheduleText({ every: ed.every }) + ", no mínimo a cada 15 min."
                  : "Roda " + U.scheduleText(ed.date !== "" && ed.days.length === 0 ? { at: at.text || "08:00", date: ed.date } : { at: at.text || "08:00", days: ed.days }) + ", horário de Brasília."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
    }

    Column {
        spacing: 8
        Overline { text: "ENTREGAR" }
        Segmented {
            value: ed.deliver
            options: [{ value: "time", label: "No #time", icon: "hash" }, { value: "conversation", label: "Em conversa própria", icon: "chat" }]
            onActivated: v => ed.deliver = v
        }
    }

    Column {
        visible: !!(ed.agent && ed.agent.responsibilities && ed.agent.responsibilities.length)
        width: parent.width
        spacing: 8
        Overline { text: "CUIDA DA RESPONSABILIDADE" }
        Flow {
            width: parent.width
            spacing: 6
            Repeater {
                model: ed.agent ? [{ id: "", goal: "Nenhuma" }].concat(ed.agent.responsibilities || []) : []
                delegate: Rectangle {
                    id: rc
                    required property var modelData
                    readonly property bool on: ed.resp === modelData.id
                    width: Math.min(rcText.implicitWidth + 26, ed.form.width)
                    height: 32
                    radius: Theme.radius
                    color: on ? Theme.white : rcArea.containsMouse ? Theme.hover : "transparent"
                    border.width: 1
                    border.color: on ? Theme.white : Theme.line
                    Text {
                        id: rcText
                        anchors.centerIn: parent
                        width: Math.min(implicitWidth, ed.form.width - 26)
                        text: rc.modelData.goal
                        elide: Text.ElideRight
                        color: rc.on ? Theme.ink : Theme.text2
                        font.pixelSize: Theme.fontBody
                    }
                    MouseArea {
                        id: rcArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: ed.resp = rc.modelData.id
                    }
                }
            }
        }
    }

    Column {
        visible: ed.skills.length > 0
        width: parent.width
        spacing: 8
        Overline { text: "SEGUE A SKILL" }
        Flow {
            width: parent.width
            spacing: 6
            Repeater {
                model: [{ id: "", name: "Nenhuma" }].concat(ed.skills)
                delegate: Rectangle {
                    id: sc
                    required property var modelData
                    readonly property bool on: ed.skill === modelData.id
                    width: Math.min(scText.implicitWidth + 26, ed.form.width)
                    height: 32
                    radius: Theme.radius
                    color: on ? Theme.white : scArea.containsMouse ? Theme.hover : "transparent"
                    border.width: 1
                    border.color: on ? Theme.white : Theme.line
                    Text {
                        id: scText
                        anchors.centerIn: parent
                        width: Math.min(implicitWidth, ed.form.width - 26)
                        text: sc.modelData.name
                        elide: Text.ElideRight
                        color: sc.on ? Theme.ink : Theme.text2
                        font.family: sc.modelData.id ? Theme.mono : Theme.sans
                        font.pixelSize: Theme.fontBody
                    }
                    MouseArea {
                        id: scArea
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: ed.skill = sc.modelData.id
                    }
                }
            }
        }
    }

    ErrorBanner {
        width: parent.width
        text: ed.error
    }

    Item {
        width: parent.width
        height: 40
        Row {
            spacing: 10
            PrimaryButton {
                text: ed.routine ? "Salvar" : "Criar rotina"
                icon: "check"
                busy: ed.busy
                onClicked: ed.save()
            }
            GhostButton {
                text: "Salvar e rodar agora"
                icon: "bolt"
                onClicked: ed.runNow()
            }
            GhostButton {
                text: "Cancelar"
                onClicked: ed.shown = false
            }
        }
        GhostButton {
            visible: ed.routine !== null
            anchors.right: parent.right
            danger: true
            text: "Excluir"
            onClicked: ed.remove()
        }
    }
}
