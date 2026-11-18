import QtQuick
import QtQuick.Controls.Basic
import Avora
import "util.js" as U

// Right column of a channel: people in the workspace (with presence) and
// agents in the channel (working / waiting / free and what they are doing).
Item {
    id: membersPane
    property var channelAgents: []
    property var channelWorking: ({})
    property var channelSteps: ({})
    property var channelNextIds: []
    property var approvals: []

    signal openAgentRequested(string id)
    signal openPersonRequested(string id)

    property var people: fallbackPeople()
    property bool loadingPeople: false

    function fallbackPeople() {
        if (Api.me && Api.me.person && Api.me.person.id) {
            const p = Api.me.person;
            return [{
                person_id: p.id,
                name: p.name || p.email || "Você",
                avatar: p.avatar || "",
                online: p.presence !== "invisible",
                presence: p.presence || "auto",
                status_text: p.status_text || ""
            }];
        }
        return [{ person_id: "self", name: "Você", avatar: "", online: true, presence: "auto", status_text: "" }];
    }

    function currentWorkspace() {
        if (Api.workspace !== "")
            return Api.workspace;
        if (Api.me && Api.me.workspaces && Api.me.workspaces.length > 0)
            return Api.me.workspaces[0].id;
        return "pessoal";
    }

    function loadMembers() {
        if (!Api.signedIn)
            return;
        const ws = currentWorkspace();
        loadingPeople = true;
        Api.get("/v1/workspaces/" + encodeURIComponent(ws) + "/members", (err, data) => {
            membersPane.loadingPeople = false;
            if (err || !Array.isArray(data)) {
                membersPane.people = membersPane.fallbackPeople();
                return;
            }
            const active = data.filter(m => m.status !== "pending");
            membersPane.people = active.length > 0 ? active : membersPane.fallbackPeople();
        });
    }

    Component.onCompleted: {
        if (visible && Api.signedIn)
            loadMembers();
    }

    onVisibleChanged: {
        if (visible && Api.signedIn)
            loadMembers();
    }

    Connections {
        target: Api
        function onWorkspaceChanged() {
            if (membersPane.visible && Api.signedIn)
                membersPane.loadMembers();
        }
        function onSignedInChanged() {
            if (membersPane.visible && Api.signedIn)
                membersPane.loadMembers();
        }
        function onLiveEvent(e) {
            if (!membersPane.visible || !e || !e.type)
                return;
            if (e.type.startsWith("workspace.") || e.type.startsWith("person.") || e.type.startsWith("invite."))
                membersPane.loadMembers();
        }
    }

    Timer {
        interval: 20000
        repeat: true
        running: membersPane.visible && Api.signedIn
        onTriggered: membersPane.loadMembers()
    }

    function personPhoto(av) {
        if (!av)
            return "";
        if (av.startsWith("avora:file/"))
            return "image://server/v1/files/" + av.slice(11);
        if (av.startsWith("/v1/files/"))
            return "image://server" + av;
        return av;
    }

    function personDotKind(m) {
        if (!m)
            return "";
        if (m.presence === "away")
            return "warn";
        if (m.online)
            return m.presence === "busy" ? "bad" : "live";
        return "";
    }

    function personStatusText(m) {
        if (!m)
            return "";
        if (m.status_text)
            return m.status_text;
        if (m.presence === "away")
            return "Ausente";
        if (m.presence === "busy")
            return "Ocupado";
        return m.online ? "Online" : "Offline";
    }

    function waitingApproval(agentId) {
        return (membersPane.approvals || []).some(a => a.agent_id === agentId);
    }

    function agentState(a) {
        if (!a)
            return { kind: "", tag: "Livre", line: "Livre", busy: false };
        const w = membersPane.channelWorking ? membersPane.channelWorking[a.id] : undefined;
        const st = w && w.job_id && membersPane.channelSteps ? membersPane.channelSteps[w.job_id] : undefined;
        const inQueue = (membersPane.channelNextIds || []).indexOf(a.id) >= 0;
        const waiting = waitingApproval(a.id) || a.status === "waiting";
        const busy = w !== undefined || inQueue || !!a.working || a.status === "working";

        if (waiting) {
            return {
                kind: "warn",
                tag: "Esperando",
                line: "Esperando sua aprovação",
                busy: busy
            };
        }
        if (busy) {
            let line = "Trabalhando…";
            if (st && st.tool)
                line = U.stepLabel(st.tool.split(" · ")[0]) + (st.summary ? " · " + st.summary : "");
            else if (w !== undefined)
                line = "Trabalhando neste canal…";
            else if (inQueue)
                line = "Na fila para responder…";
            else if (a.status_channel)
                line = "Trabalhando em #" + String(a.status_channel).replace(/^#/, "");
            else if (a.status_text)
                line = a.status_text;
            return {
                kind: "live",
                tag: "Trabalhando",
                line: line,
                busy: true
            };
        }
        return {
            kind: "",
            tag: "Livre",
            line: a.status_text || a.role || "Livre",
            busy: false
        };
    }

    Flickable {
        id: flick
        anchors.fill: parent
        contentHeight: col.implicitHeight + 2 * Theme.space5
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: SlimScrollBar {}
        SmoothWheel {}

        Column {
            id: col
            x: Theme.space3
            y: Theme.space3
            width: flick.width - 2 * Theme.space3
            spacing: Theme.space5

            // ── People ──────────────────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space1

                Overline {
                    leftPadding: Theme.space2
                    bottomPadding: Theme.space1
                    text: "PESSOAS  ·  " + membersPane.people.length
                }

                Repeater {
                    model: membersPane.people
                    delegate: Rectangle {
                        id: personRow
                        required property var modelData
                        readonly property string pid: modelData.person_id || modelData.id || "self"
                        width: col.width
                        height: 52
                        radius: Theme.radiusMd
                        color: personArea.pressed ? Theme.active : personArea.containsMouse ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Avatar {
                            id: personFace
                            x: Theme.space2
                            anchors.verticalCenter: parent.verticalCenter
                            size: 36
                            name: personRow.modelData.name || "Você"
                            photo: membersPane.personPhoto(personRow.modelData.avatar)
                            tint: Theme.text2
                        }

                        Column {
                            anchors {
                                left: personFace.right
                                leftMargin: Theme.space3
                                right: parent.right
                                rightMargin: Theme.space3
                                verticalCenter: parent.verticalCenter
                            }
                            spacing: 2

                            Row {
                                width: parent.width
                                spacing: Theme.space2

                                Text {
                                    text: personRow.modelData.name || "Você"
                                    color: Theme.text
                                    font.pixelSize: Theme.fontBody
                                    font.weight: Font.DemiBold
                                    elide: Text.ElideRight
                                    width: Math.min(implicitWidth, parent.width - 20)
                                }

                                Dot {
                                    anchors.verticalCenter: parent.verticalCenter
                                    kind: membersPane.personDotKind(personRow.modelData)
                                }
                            }

                            Text {
                                width: parent.width
                                text: membersPane.personStatusText(personRow.modelData)
                                color: Theme.text3
                                font.pixelSize: Theme.fontSmall
                                elide: Text.ElideRight
                            }
                        }

                        MouseArea {
                            id: personArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (typeof win !== "undefined" && win && typeof win.openPerson === "function")
                                    win.openPerson(personRow.pid);
                                else
                                    membersPane.openPersonRequested(personRow.pid);
                            }
                        }
                    }
                }
            }

            // ── Channel agents ──────────────────────────────────
            Column {
                width: parent.width
                spacing: Theme.space1

                Overline {
                    leftPadding: Theme.space2
                    bottomPadding: Theme.space1
                    text: "AGENTES  ·  " + membersPane.channelAgents.length
                }

                Repeater {
                    model: membersPane.channelAgents
                    delegate: Rectangle {
                        id: agentRow
                        required property var modelData
                        readonly property var st: membersPane.agentState(modelData)
                        width: col.width
                        height: 58
                        radius: Theme.radiusMd
                        color: agentArea.pressed ? Theme.active : agentArea.containsMouse ? Theme.hover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.fast } }

                        Avatar {
                            id: agentFace
                            x: Theme.space2
                            anchors.verticalCenter: parent.verticalCenter
                            size: 36
                            name: agentRow.modelData.name || "?"
                            photo: U.photo(agentRow.modelData)
                            tint: agentRow.modelData.color || Theme.text2
                            working: agentRow.st.busy
                        }

                        Column {
                            anchors {
                                left: agentFace.right
                                leftMargin: Theme.space3
                                right: parent.right
                                rightMargin: Theme.space3
                                verticalCenter: parent.verticalCenter
                            }
                            spacing: 2

                            Item {
                                width: parent.width
                                height: 18

                                Row {
                                    anchors { left: parent.left; right: stateLabel.left; rightMargin: Theme.space2; verticalCenter: parent.verticalCenter }
                                    spacing: Theme.space2

                                    Text {
                                        text: agentRow.modelData.name || ""
                                        color: Theme.white
                                        font.pixelSize: Theme.fontBody
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                        width: Math.min(implicitWidth, parent.width - 18)
                                    }

                                    Dot {
                                        anchors.verticalCenter: parent.verticalCenter
                                        kind: agentRow.st.kind
                                    }
                                }

                                Text {
                                    id: stateLabel
                                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                                    text: agentRow.st.tag
                                    color: agentRow.st.kind === "warn" ? Theme.warn
                                         : agentRow.st.kind === "live" ? Theme.ok
                                         : Theme.text4
                                    font.pixelSize: Theme.fontCaption
                                }
                            }

                            Text {
                                width: parent.width
                                text: agentRow.st.line
                                color: agentRow.st.kind === "warn" ? Theme.warn
                                     : agentRow.st.busy ? Theme.text2
                                     : Theme.text3
                                font.pixelSize: Theme.fontSmall
                                elide: Text.ElideRight
                            }
                        }

                        MouseArea {
                            id: agentArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: {
                                if (typeof win !== "undefined" && win && typeof win.openAgent === "function")
                                    win.openAgent(agentRow.modelData.id);
                                else
                                    membersPane.openAgentRequested(agentRow.modelData.id);
                            }
                        }
                    }
                }

                Rectangle {
                    visible: membersPane.channelAgents.length === 0
                    width: parent.width
                    height: emptyAgentsText.implicitHeight + 2 * Theme.space3
                    radius: Theme.radiusMd
                    color: Theme.wash1
                    border.width: 1
                    border.color: Theme.line

                    Text {
                        id: emptyAgentsText
                        anchors.centerIn: parent
                        width: parent.width - 2 * Theme.space3
                        horizontalAlignment: Text.AlignHCenter
                        wrapMode: Text.Wrap
                        text: "Nenhum agente neste canal ainda. Abra as configurações do canal para escolher quem participa."
                        color: Theme.text3
                        font.pixelSize: Theme.fontSmall
                    }
                }
            }
        }
    }
}
