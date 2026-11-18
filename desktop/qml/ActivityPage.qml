import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// The audit log: everything Avora did, newest first, live.
Item {
    id: activity
    property string filter: "all"
    property var all: []

    readonly property var labels: ({
        "job.created": "Tarefa criada",
        "job.claimed": "Tarefa iniciada",
        "job.succeeded": "Tarefa concluída",
        "job.failed": "Tarefa falhou",
        "job.canceled": "Tarefa cancelada",
        "job.requeued": "Tarefa voltou à fila",
        "job.timed_out": "Tarefa expirou",
        "agent.step": "Passo do agente",
        "agent.memory_saved": "Memória guardada",
        "agent.memory_deleted": "Memória apagada",
        "node.online": "Nó online",
        "node.paired": "Nó pareado",
        "node.revoked": "Nó revogado",
        "node.pairing_code_created": "Código de pareamento",
        "conversation.created": "Conversa criada",
        "message.created": "Mensagem",
        "llm.failed": "Modelo falhou"
    })

    function inGroup(t) {
        switch (filter) {
        case "jobs": return t.startsWith("job.");
        case "agent": return t.startsWith("agent.");
        case "chat": return t.startsWith("conversation.") || t.startsWith("message.") || t.startsWith("llm.");
        case "nodes": return t.startsWith("node.");
        default: return true;
        }
    }

    function dotFor(t) {
        if (t.endsWith("failed") || t.endsWith("timed_out") || t.endsWith("revoked"))
            return "bad";
        if (t.endsWith("succeeded") || t === "node.online" || t === "node.paired")
            return "ok";
        if (t === "agent.step" || t === "job.claimed")
            return "live";
        return "";
    }

    function describe(e) {
        const p = e.payload || {};
        switch (e.type) {
        case "job.created":
            return [p.action, p.target && "→ " + p.target, p.reason].filter(x => x).join(" · ");
        case "agent.step":
            return (p.tool || "") + " " + (p.summary || "");
        case "job.succeeded":
        case "job.failed": {
            const s = p.stats;
            return [p.error, s && ((s.engine === "shell" ? "sandbox" : s.engine) + " " + (s.model || "")), s && U.seconds(s.duration_ms), s && s.turns + " turnos"].filter(x => x).join(" · ");
        }
        case "message.created":
            return (p.role === "user" ? "Você" : "Avora") + " · " + p.chars + " caracteres" + (p.completion_tokens ? " · " + p.completion_tokens + " tokens · " + U.seconds(p.duration_ms) : "");
        case "agent.memory_saved":
        case "agent.memory_deleted":
            return [p.agent_id, p.source, p.chars && p.chars + " caracteres"].filter(x => x).join(" · ");
        case "llm.failed":
            return ((p.status || "") + " " + (p.error || "")).trim();
        case "conversation.created":
            return (p.engine || "") + " · " + (p.model || "");
        case "node.pairing_code_created":
            return [p.name, p.kind, p.expires_at && "vale até " + U.clock(p.expires_at)].filter(x => x).join(" · ");
        case "node.paired":
        case "node.online":
            return [p.name, p.kind].filter(x => x).join(" · ");
        default:
            return Object.keys(p).filter(k => p[k] !== "" && p[k] !== null && typeof p[k] !== "object").map(k => k + ": " + p[k]).join(" · ");
        }
    }

    function rebuild() {
        const rows = [];
        for (const e of all) {
            if (!inGroup(e.type))
                continue;
            rows.push({
                id: e.id,
                day: U.day(e.at),
                time: U.clock(e.at),
                label: labels[e.type] || e.type,
                detail: describe(e),
                dot: dotFor(e.type),
                actor: (e.actor || "").replace("node:", "")
            });
        }
        U.syncModel(events, rows);
    }

    onFilterChanged: rebuild()

    ListModel { id: events }

    Timer {
        interval: 4000
        repeat: true
        running: activity.visible
        triggeredOnStart: true
        onTriggered: Api.get("/v1/events?order=latest&limit=300", (err, data) => {
            if (err)
                return;
            if (!U.same(activity.all, data))
                activity.all = data;
            activity.rebuild();
        })
    }

    PageHead {
        id: head
        x: Theme.gutter
        y: 52
        eyebrow: "Auditoria"
        title: "Atividade"
        sub: "Tudo o que o Avora fez, em ordem. O registro não pode ser apagado nem editado."
    }

    Item {
        id: bar
        anchors { top: head.bottom; topMargin: 26; left: parent.left; right: parent.right; leftMargin: Theme.gutter; rightMargin: Theme.gutter }
        height: 38
        Segmented {
            value: activity.filter
            options: [
                { value: "all", label: "Tudo" },
                { value: "jobs", label: "Tarefas" },
                { value: "agent", label: "Agentes" },
                { value: "chat", label: "Chat" },
                { value: "nodes", label: "Nós" }
            ]
            onActivated: v => activity.filter = v
        }
        Rectangle {
            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
            height: 26
            width: live.implicitWidth + 22
            radius: Theme.radiusMd
            color: Theme.hover
            border.width: 1
            border.color: Theme.line
            Row {
                id: live
                anchors.centerIn: parent
                spacing: 7
                Dot { kind: "live"; anchors.verticalCenter: parent.verticalCenter }
                Text { text: "ao vivo"; color: Theme.text2; font.family: Theme.mono; font.pixelSize: Theme.fontSmall; anchors.verticalCenter: parent.verticalCenter }
            }
        }
    }

    Card {
        anchors { top: bar.bottom; topMargin: 18; bottom: parent.bottom; bottomMargin: 28; left: parent.left; right: parent.right; leftMargin: Theme.gutter; rightMargin: Theme.gutter }
        padding: 6

        ListView {
            id: list
            anchors.fill: parent
            clip: true
            model: events
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: SlimScrollBar {}
            SmoothWheel {}
            section.property: "day"
            section.delegate: Text {
                required property string section
                text: section.toUpperCase()
                leftPadding: 18
                topPadding: 16
                bottomPadding: 6
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
                font.letterSpacing: Theme.trackCaps
            }
            delegate: Rectangle {
                id: ev
                required property var model
                width: list.width
                height: 44
                radius: Theme.radiusMd
                color: ea.containsMouse ? Theme.hover : "transparent"
                MouseArea { id: ea; anchors.fill: parent; hoverEnabled: true }

                Text {
                    id: when
                    x: 18
                    anchors.verticalCenter: parent.verticalCenter
                    text: ev.model.time
                    color: Theme.text4
                    font.family: Theme.mono
                    font.pixelSize: Theme.fontSmall
                }
                Row {
                    id: what
                    x: 104
                    width: 210
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 10
                    Dot { kind: ev.model.dot; anchors.verticalCenter: parent.verticalCenter }
                    Text {
                        text: ev.model.label
                        color: Theme.text
                        font.pixelSize: Theme.fontLarge
                        font.weight: Font.Medium
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
                Text {
                    anchors { left: what.right; leftMargin: 12; right: who.left; rightMargin: 16; verticalCenter: parent.verticalCenter }
                    text: ev.model.detail
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                    elide: Text.ElideRight
                }
                Text {
                    id: who
                    anchors { right: parent.right; rightMargin: 18; verticalCenter: parent.verticalCenter }
                    text: ev.model.actor
                    color: Theme.text4
                    font.family: Theme.mono
                    font.pixelSize: Theme.fontSmall
                }
            }
            Text {
                visible: events.count === 0
                anchors.centerIn: parent
                text: "Nada por aqui ainda."
                color: Theme.text4
                font.pixelSize: Theme.fontLarge
            }
        }
    }
}
