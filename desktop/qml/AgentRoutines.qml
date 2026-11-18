import QtQuick
import Avora
import "util.js" as U

// An agent's responsibilities and routines, for the profile panel: what it
// keeps an eye on, when it runs on its own and how the last run went.
Column {
    id: box
    property var agent: null
    property var routines: []
    signal edit(var routine)
    signal create(var preset)
    signal changed

    spacing: 18

    readonly property color tint: agent && agent.color ? agent.color : Theme.text2

    function statusColor(r) {
        if (!r.enabled)
            return Theme.text4;
        switch (r.last_status) {
        case "ok": return Theme.ok;
        case "failed": return Theme.bad;
        case "running": return tint;
        default: return Theme.text3;
        }
    }

    function statusText(r) {
        const prev = (r.last_preview || "").replace(/[*_`#>]/g, "");
        r = Object.assign({}, r, { last_preview: prev });
        switch (r.last_status) {
        case "running": return "rodando agora…";
        case "ok": return "ok " + U.ago(r.last_run) + (r.last_preview ? ": " + r.last_preview : "");
        case "failed": return "falhou " + U.ago(r.last_run) + (r.failures > 1 ? " (" + r.failures + "× seguidas)" : "") + ": " + r.last_preview;
        case "skipped": return "pulou " + U.ago(r.last_run) + ": " + r.last_preview;
        default: return "ainda não rodou";
        }
    }

    function linked(respId) {
        return routines.find(r => r.responsibility_id === respId) || null;
    }

    function toggle(r) {
        const body = { agent_id: r.agent_id, title: r.title, prompt: r.prompt, schedule: r.schedule, deliver: r.deliver,
                       responsibility_id: r.responsibility_id || "", enabled: !r.enabled };
        Api.put("/v1/routines/" + r.id, body, () => box.changed());
    }

    // ── Responsibilities ────────────────────────────────────────
    Column {
        width: parent.width
        spacing: 8
        Overline { text: "RESPONSABILIDADES" }
        Text {
            visible: !box.agent || !box.agent.responsibilities || box.agent.responsibilities.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Nenhuma ainda. Peça na conversa, por exemplo: «cuide para meus gastos ficarem abaixo do orçamento»."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Repeater {
            model: box.agent ? box.agent.responsibilities || [] : []
            delegate: Column {
                id: rs
                required property var modelData
                readonly property var routine: box.linked(modelData.id)
                width: box.width
                spacing: 3
                Row {
                    spacing: 8
                    Rectangle {
                        width: 7
                        height: 7
                        radius: 3.5
                        y: 6
                        color: rs.routine ? box.statusColor(rs.routine) : Theme.text4
                    }
                    Text {
                        width: box.width - 15
                        text: rs.modelData.goal
                        color: Theme.text
                        font.pixelSize: Theme.fontBody
                        wrapMode: Text.Wrap
                    }
                }
                Text {
                    visible: text !== ""
                    leftPadding: 15
                    width: box.width
                    text: [rs.modelData.metric, rs.modelData.cadence].filter(x => !!x).join("  ·  ")
                    color: Theme.text4
                    font.pixelSize: Theme.fontSmall
                    wrapMode: Text.Wrap
                }
                Text {
                    leftPadding: 15
                    width: box.width
                    text: rs.routine ? box.statusText(rs.routine) : "sem rotina cuidando"
                    color: rs.routine && rs.routine.last_status === "failed" ? Theme.bad : Theme.text3
                    font.pixelSize: Theme.fontSmall
                    maximumLineCount: 2
                    elide: Text.ElideRight
                    wrapMode: Text.Wrap
                }
                Text {
                    visible: !rs.routine
                    leftPadding: 15
                    text: "+ criar rotina"
                    color: newArea.containsMouse ? Theme.white : Theme.text2
                    font.pixelSize: Theme.fontSmall
                    font.underline: newArea.containsMouse
                    MouseArea {
                        id: newArea
                        anchors.fill: parent
                        anchors.margins: -4
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: box.create({
                            title: rs.modelData.goal.slice(0, 80),
                            prompt: "Cheque a responsabilidade «" + rs.modelData.goal + "»" +
                                    (rs.modelData.metric ? " (métrica: " + rs.modelData.metric + ")" : "") +
                                    ". Diga o status (ok, atenção ou fora), o número atual e o próximo passo, em até 5 linhas.",
                            schedule: { at: "08:00", days: [] },
                            deliver: "time",
                            responsibility_id: rs.modelData.id
                        })
                    }
                }
            }
        }
    }

    // ── Routines ────────────────────────────────────────────────
    Column {
        width: parent.width
        spacing: 6
        Item {
            width: parent.width
            height: 26
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: "ROTINAS"
                color: Theme.text4
                font.pixelSize: Theme.fontCaption
                font.weight: Font.DemiBold
                font.letterSpacing: Theme.trackCaps
            }
            GhostButton {
                anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                small: true
                icon: "plus"
                text: "Nova"
                onClicked: box.create(null)
            }
        }
        Text {
            visible: box.routines.length === 0
            width: parent.width
            wrapMode: Text.Wrap
            text: "Nenhuma ainda. Peça na conversa, por exemplo: «toda segunda às 9, revise meus gastos». " + (box.agent ? box.agent.name : "Ele") + " cria e cuida dela."
            color: Theme.text4
            font.pixelSize: Theme.fontSmall
        }
        Repeater {
            model: box.routines
            delegate: Rectangle {
                id: rt
                required property var modelData
                readonly property var r: modelData
                width: box.width
                height: rtCol.implicitHeight + 20
                radius: Theme.radiusMd
                color: rtArea.containsMouse ? Theme.hover : Theme.wash1
                border.width: 1
                border.color: Theme.line
                opacity: rt.r.enabled ? 1 : 0.6
                MouseArea {
                    id: rtArea
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: box.edit(rt.r)
                }
                Column {
                    id: rtCol
                    x: 12
                    y: 10
                    width: parent.width - 66
                    spacing: 3
                    Row {
                        spacing: 7
                        Rectangle {
                            width: 7
                            height: 7
                            radius: 3.5
                            anchors.verticalCenter: parent.verticalCenter
                            color: box.statusColor(rt.r)
                            SequentialAnimation on opacity {
                                running: rt.r.last_status === "running" && parent.visible
                                loops: Animation.Infinite
                                NumberAnimation { to: 0.3; duration: 600 }
                                NumberAnimation { to: 1; duration: 600 }
                            }
                        }
                        Text {
                            width: rtCol.width - 14
                            text: rt.r.title
                            color: Theme.text
                            font.pixelSize: Theme.fontBody
                            font.weight: Font.Medium
                            elide: Text.ElideRight
                        }
                    }
                    Text {
                        width: rtCol.width
                        text: U.whenText(rt.r) + (rt.r.deliver === "time" ? "  ·  #time" : "  ·  conversa") +
                              (rt.r.enabled && rt.r.next_run ? "\npróxima: " + U.when(rt.r.next_run) : rt.r.enabled ? "" : "\ndesligada")
                        color: Theme.text4
                        font.pixelSize: Theme.fontSmall
                        lineHeight: 1.15
                    }
                    Text {
                        visible: rt.r.last_status !== undefined && rt.r.last_status !== ""
                        width: rtCol.width
                        text: box.statusText(rt.r)
                        color: rt.r.last_status === "failed" ? Theme.bad : Theme.text3
                        font.pixelSize: Theme.fontSmall
                        maximumLineCount: 2
                        elide: Text.ElideRight
                        wrapMode: Text.Wrap
                    }
                }
                // On/off.
                Rectangle {
                    id: sw
                    anchors { right: parent.right; rightMargin: 12; top: parent.top; topMargin: 12 }
                    width: 34
                    height: 20
                    radius: Theme.radiusMd
                    color: rt.r.enabled ? Theme.white : Theme.wash4
                    border.width: 1
                    border.color: rt.r.enabled ? Theme.white : Theme.line
                    Behavior on color { ColorAnimation { duration: Theme.fast } }
                    Rectangle {
                        width: 14
                        height: 14
                        radius: Theme.radiusSm
                        y: 3
                        x: rt.r.enabled ? parent.width - width - 3 : 3
                        color: rt.r.enabled ? Theme.ink : Theme.text3
                        Behavior on x { NumberAnimation { duration: Theme.fast } }
                    }
                    MouseArea {
                        anchors.fill: parent
                        anchors.margins: -6
                        cursorShape: Qt.PointingHandCursor
                        onClicked: box.toggle(rt.r)
                    }
                }
            }
        }
    }
}
