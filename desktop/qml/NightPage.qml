import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Turno da noite (M5.1): the tasks left for the night, from the chat, from
// here or tagged in ClickUp. At the shift the chief of staff does them one
// by one and writes the report in #time: done, waiting for you, stuck.
Item {
    id: page
    property var nv: ({ tasks: [] })
    property string error: ""
    property bool busy: false

    function load() {
        Api.get("/v1/night", (err, data) => {
            if (!err)
                nv = data;
        });
    }
    function save(patch) {
        const body = Object.assign({ enabled: nv.enabled, agent_id: nv.agent_id, start: nv.start, end: nv.end,
                                     max_tasks: nv.max_tasks, clickup_tag: nv.clickup_tag }, patch || {});
        Api.put("/v1/night", body, (err, data) => {
            error = err || "";
            if (!err)
                nv = data;
        });
    }
    function add() {
        const t = newTask.text.trim();
        if (t === "")
            return;
        busy = true;
        Api.post("/v1/night/tasks", { text: t }, (err, data) => {
            busy = false;
            error = err || "";
            if (!err) {
                nv = data;
                newTask.text = "";
            }
        });
    }
    function remove(id) {
        Api.del("/v1/night/tasks/" + id, err => {
            error = err || "";
            load();
        });
    }
    function runNow() {
        Api.post("/v1/night/run", {}, (err, data) => {
            error = err || "";
            if (!err)
                nv = data;
        });
    }
    readonly property var statusText: ({ pendente: "Na fila", fazendo: "Fazendo agora", feito: "Feito", esperando: "Esperando você", travado: "Travado" })
    readonly property var statusTone: ({ pendente: Theme.text3, fazendo: Theme.white, feito: "#4ADE80", esperando: "#FBBF24", travado: Theme.bad })
    readonly property var tasks: (nv.tasks || []).slice().sort((a, b) => {
        const rank = s => ({ fazendo: 0, pendente: 1 })[s] ?? 2;
        return rank(a.status) - rank(b.status) || (b.done_at || b.created_at).localeCompare(a.done_at || a.created_at);
    })
    readonly property int pending: (nv.tasks || []).filter(t => t.status === "pendente").length

    Timer {
        interval: 5000
        repeat: true
        running: page.visible
        triggeredOnStart: true
        onTriggered: page.load()
    }

    Flickable {
        id: scroller
        anchors.fill: parent
        contentHeight: col.height + 110
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            x: Theme.gutter
            y: 52
            width: Math.min(scroller.width - 2 * Theme.gutter, 760)
            spacing: 26

            PageHead {
                eyebrow: "Proatividade"
                title: "Turno da noite"
                sub: "O que você deixa pra noite a Vega faz de madrugada, sozinha. De manhã o relatório está no #time: feito, esperando você, travado."
            }

            Card {
                width: parent.width
                height: head.implicitHeight + 2 * padding
                padding: 22
                Column {
                    id: head
                    width: parent.width
                    spacing: 14
                    Item {
                        width: parent.width
                        height: 32
                        Row {
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 10
                            Dot { kind: page.nv.running ? "live" : ""; anchors.verticalCenter: parent.verticalCenter }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: page.nv.running ? "Trabalhando agora" : page.nv.enabled ? "Ligado · " + (page.nv.start || "") + " às " + (page.nv.end || "") : "Desligado"
                                color: Theme.white
                                font.pixelSize: Theme.fontTitle
                                font.weight: Font.DemiBold
                            }
                        }
                        Segmented {
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            options: [{ value: "on", label: "Ligado" }, { value: "off", label: "Desligado" }]
                            value: page.nv.enabled ? "on" : "off"
                            onActivated: v => page.save({ enabled: v === "on" })
                        }
                    }
                    Text {
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: Theme.text2
                        font.pixelSize: Theme.fontBody
                        lineHeight: 1.3
                        text: page.pending + " tarefa(s) na fila. Até " + (page.nv.max_tasks || 0) + " por noite; o resto fica para a próxima." +
                              (page.nv.clickup_tag ? " Tarefas do ClickUp com a etiqueta «" + page.nv.clickup_tag + "» entram sozinhas." : "") +
                              " De noite nada que precisa de aprovação roda: ela deixa pronto e te espera."
                    }
                    GhostButton {
                        icon: "bolt"
                        text: page.nv.running ? "Rodando…" : "Rodar agora"
                        enabled: !page.nv.running && page.pending > 0
                        onClicked: page.runNow()
                    }
                }
            }

            Column {
                width: parent.width
                spacing: 12
                Text { text: "Começa às"; color: Theme.text3; font.pixelSize: Theme.fontSmall; font.weight: Font.Medium }
                Segmented {
                    options: ["23:00", "00:00", "01:00", "02:00", "03:00"].map(h => ({ value: h, label: h }))
                    value: page.nv.start || "01:00"
                    onActivated: v => page.save({ start: v })
                }
                Text { text: "Tarefas por noite"; color: Theme.text3; font.pixelSize: Theme.fontSmall; font.weight: Font.Medium }
                Segmented {
                    options: [{ value: "3", label: "3" }, { value: "5", label: "5" }, { value: "10", label: "10" }]
                    value: String(page.nv.max_tasks || 5)
                    onActivated: v => page.save({ max_tasks: Number(v) })
                }
                Text { text: "Etiqueta do ClickUp (vazio = não pega do ClickUp)"; color: Theme.text3; font.pixelSize: Theme.fontSmall; font.weight: Font.Medium }
                Field {
                    width: 240
                    text: page.nv.clickup_tag || ""
                    placeholderText: "avora"
                    onAccepted: page.save({ clickup_tag: text.trim() })
                    onActiveFocusChanged: if (!activeFocus && text.trim() !== (page.nv.clickup_tag || "")) page.save({ clickup_tag: text.trim() })
                }
            }

            Column {
                width: parent.width
                spacing: 12
                Text { text: "Fila"; color: Theme.white; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold }
                Item {
                    width: parent.width
                    height: newTask.height
                    AreaField {
                        id: newTask
                        anchors { left: parent.left; right: addBtn.left; rightMargin: 10 }
                        minHeight: 46
                        placeholderText: "Deixar pra noite… (Enter adiciona)"
                        onSubmitted: page.add()
                    }
                    PrimaryButton {
                        id: addBtn
                        anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                        text: "Adicionar"
                        busy: page.busy
                        enabled: newTask.text.trim() !== "" && !page.busy
                        onClicked: page.add()
                    }
                }
                Text {
                    visible: page.tasks.length === 0
                    text: "Nada na fila. No chat, é só dizer \"deixa isso pra noite\"."
                    color: Theme.text4
                    font.pixelSize: Theme.fontBody
                }
                Repeater {
                    model: page.tasks
                    delegate: Card {
                        id: tc
                        required property var modelData
                        width: col.width
                        height: tcol.implicitHeight + 2 * padding
                        padding: 16
                        radius: Theme.radiusLg
                        Column {
                            id: tcol
                            width: parent.width - (rm.visible ? rm.width + 10 : 0)
                            spacing: 6
                            Row {
                                spacing: 8
                                Text {
                                    text: page.statusText[tc.modelData.status] || tc.modelData.status
                                    color: page.statusTone[tc.modelData.status] || Theme.text3
                                    font.pixelSize: Theme.fontSmall
                                    font.weight: Font.DemiBold
                                }
                                Text {
                                    text: (tc.modelData.source === "clickup" ? "ClickUp" : tc.modelData.source === "chat" ? "pelo chat" : "pelo app") +
                                          " · " + U.ago(tc.modelData.done_at || tc.modelData.created_at)
                                    color: Theme.text4
                                    font.pixelSize: Theme.fontSmall
                                }
                            }
                            Text {
                                width: parent.width
                                text: tc.modelData.text
                                color: Theme.text
                                font.pixelSize: Theme.fontLarge
                                wrapMode: Text.Wrap
                                maximumLineCount: 4
                                elide: Text.ElideRight
                            }
                            Text {
                                visible: !!tc.modelData.result
                                width: parent.width
                                text: tc.modelData.result || ""
                                color: Theme.text2
                                font.pixelSize: Theme.fontBody
                                wrapMode: Text.Wrap
                            }
                        }
                        GhostButton {
                            id: rm
                            visible: tc.modelData.status !== "fazendo"
                            anchors { right: parent.right; top: parent.top }
                            small: true
                            icon: "close"
                            text: tc.modelData.status === "pendente" ? "Tirar" : "Limpar"
                            onClicked: page.remove(tc.modelData.id)
                        }
                    }
                }
            }

            ErrorBanner {
                width: parent.width
                visible: text !== ""
                text: page.error
            }
        }
    }
}
