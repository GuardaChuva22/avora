import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Heartbeat (M4): every so often the chief of staff looks at what may need
// the owner (e-mails without an answer, the next meetings, tasks due,
// approvals stuck, routines that failed) and tells #time only what is new.
// Here: on or off, how often, the daily budget, and the owner's checklist.
Item {
    id: page
    property var hb: ({})
    property bool dirty: false
    property bool saving: false
    property bool running: false
    property string error: ""

    function load() {
        Api.get("/v1/heartbeat", (err, data) => {
            if (err) {
                error = err;
                return;
            }
            hb = data;
            if (!dirty)
                checklist.text = data.checklist || "";
        });
    }
    function save(patch) {
        saving = true;
        const body = Object.assign({ enabled: hb.enabled, agent_id: hb.agent_id, every: hb.every, budget: hb.budget,
                                     checklist: checklist.text }, patch || {});
        Api.put("/v1/heartbeat", body, (err, data) => {
            saving = false;
            error = err || "";
            if (!err) {
                hb = data;
                dirty = false;
                checklist.text = data.checklist || "";
            }
        });
    }
    function runNow() {
        running = true;
        Api.post("/v1/heartbeat/run", {}, (err, data) => {
            running = false;
            error = err || "";
            if (!err)
                hb = data;
        });
    }
    readonly property var st: hb.state || ({})
    readonly property var decisions: ({
        "nada": "Nada novo", "avisou": "Avisou no #time", "conferindo": "Conferindo agora", "sem orçamento": "Sem orçamento hoje",
        "bloqueado": "Parado", "ocupado": "Ocupado", "erro": "Erro"
    })

    Timer {
        interval: 6000
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
                title: "Heartbeat"
                sub: "De tempos em tempos a Vega confere o que pode precisar de você e avisa no #time só o que é novo, sem você pedir."
            }

            Card {
                width: parent.width
                height: status.implicitHeight + 2 * padding
                padding: 22
                Column {
                    id: status
                    width: parent.width
                    spacing: 14
                    Item {
                        width: parent.width
                        height: 32
                        Row {
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 10
                            Dot { kind: page.hb.enabled ? "live" : ""; anchors.verticalCenter: parent.verticalCenter }
                            Text {
                                anchors.verticalCenter: parent.verticalCenter
                                text: page.hb.enabled ? "Ligado" : "Desligado"
                                color: Theme.white
                                font.pixelSize: Theme.fontTitle
                                font.weight: Font.DemiBold
                            }
                        }
                        Segmented {
                            anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                            options: [{ value: "on", label: "Ligado" }, { value: "off", label: "Desligado" }]
                            value: page.hb.enabled ? "on" : "off"
                            onActivated: v => page.save({ enabled: v === "on" })
                        }
                    }
                    Text {
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: Theme.text2
                        font.pixelSize: Theme.fontBody
                        lineHeight: 1.3
                        text: !page.st.last_run ? "Ainda não conferiu. Confere das 7h às 22h."
                            : "Última conferência " + U.ago(page.st.last_run) + ": " + (page.decisions[page.st.last_decision] || page.st.last_decision || "") +
                              (page.st.last_note ? " · " + page.st.last_note : "") +
                              "\nHoje acordou a Vega " + (page.st.used || 0) + " de " + (page.hb.budget || 0) + " vezes. Confere das 7h às 22h."
                    }
                    GhostButton {
                        icon: "refresh"
                        text: page.running ? "Conferindo…" : "Conferir agora"
                        enabled: !page.running
                        onClicked: page.runNow()
                    }
                }
            }

            Column {
                width: parent.width
                spacing: 12
                Text { text: "A cada"; color: Theme.text3; font.pixelSize: Theme.fontSmall; font.weight: Font.Medium }
                Segmented {
                    options: [{ value: "15", label: "15 min" }, { value: "30", label: "30 min" }, { value: "60", label: "1 h" }, { value: "120", label: "2 h" }]
                    value: String(page.hb.every || 30)
                    onActivated: v => page.save({ every: Number(v) })
                }
                Text { text: "Quantas vezes por dia ela pode acordar (o modelo só roda quando aparece algo novo)"; color: Theme.text3; font.pixelSize: Theme.fontSmall; font.weight: Font.Medium }
                Segmented {
                    options: [{ value: "6", label: "6" }, { value: "12", label: "12" }, { value: "24", label: "24" }, { value: "48", label: "48" }]
                    value: String(page.hb.budget || 12)
                    onActivated: v => page.save({ budget: Number(v) })
                }
            }

            Column {
                width: parent.width
                spacing: 10
                Text { text: "O que importa para você"; color: Theme.white; font.pixelSize: Theme.fontLarge; font.weight: Font.DemiBold }
                Text {
                    width: parent.width
                    wrapMode: Text.Wrap
                    text: "A Vega decide por esta lista o que merece um aviso. Ela olha os e-mails sem resposta há 2 dias, a agenda das próximas 2 horas, as tarefas do ClickUp vencendo, as DMs do Instagram, as aprovações paradas e as rotinas que falharam. Ela pode deixar rascunhos prontos, mas nunca envia nada sem você."
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                    lineHeight: 1.3
                }
                AreaField {
                    id: checklist
                    width: parent.width
                    minHeight: 180
                    maxHeight: 360
                    enterSubmits: false
                    placeholderText: "Uma linha por coisa que importa…"
                    area.onTextChanged: if (area.activeFocus) page.dirty = true
                }
                Row {
                    spacing: 10
                    PrimaryButton {
                        text: page.saving ? "Salvando…" : "Salvar"
                        busy: page.saving
                        enabled: page.dirty && !page.saving
                        onClicked: page.save()
                    }
                    GhostButton {
                        text: "Voltar ao padrão"
                        visible: checklist.text !== (page.hb.default_checklist || "")
                        onClicked: {
                            checklist.text = page.hb.default_checklist || "";
                            page.dirty = true;
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
