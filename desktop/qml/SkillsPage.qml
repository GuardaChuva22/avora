import QtQuick
import QtQuick.Controls.Basic
import Avora

// Skills: written know-how the agents follow (the SKILL.md format). Each
// agent sees the name and description of the skills of its project and
// the team's, and loads the steps when a request matches. Below them, the
// programs installed on each node that the skills use (yt-dlp, ffmpeg...).
Item {
    id: page
    property var skills: []
    property var starters: []
    property var projects: []
    property var agents: []
    property var tools: []
    property string error: ""
    property string busy: ""

    readonly property var sections: {
        const out = [{ id: "", name: "Todo o time", color: "", list: skills.filter(s => !s.project_id) }];
        for (const p of projects) {
            const list = skills.filter(s => s.project_id === p.id);
            if (list.length)
                out.push({ id: p.id, name: p.name, color: p.color, list: list });
        }
        return out;
    }
    // sheet opens the editor at start (deep links, QA): "nova", "colar" or
    // the id of a skill.
    property string sheet: ""
    onSkillsChanged: {
        if (sheet === "")
            return;
        const s = skills.find(x => x.id === sheet);
        if (sheet === "nova" || sheet === "colar")
            editor.open(null, sheet === "colar");
        else if (s)
            editor.open(s, false);
        sheet = "";
    }
    readonly property var missingStarters: starters.filter(st => !skills.some(s => s.id === st.id))

    function load() {
        Api.get("/v1/skills", (err, data) => {
            if (err) {
                error = err;
                return;
            }
            error = "";
            skills = data;
        });
        Api.get("/v1/skills/starters", (err, data) => { if (!err) starters = data; });
        Api.get("/v1/projects", (err, data) => { if (!err) projects = data; });
        Api.get("/v1/agents", (err, data) => { if (!err) agents = data; });
        Api.get("/v1/tools", (err, data) => { if (!err) tools = data; });
    }
    Component.onCompleted: load()

    function author(a) {
        if (!a || a === "admin")
            return "você";
        if (a.startsWith("agent:")) {
            const ag = agents.find(x => x.id === a.slice(6));
            return ag ? ag.name : a.slice(6);
        }
        return a;
    }

    function addStarter(st) {
        busy = st.id;
        Api.post("/v1/skills", { name: st.name, description: st.description, body: st.body }, err => {
            busy = "";
            if (err)
                error = err;
            load();
        });
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
            // Centered, with even margins on both sides.
            x: Math.round((scroller.width - width) / 2)
            y: 52
            width: Math.min(scroller.width - 2 * Theme.gutter, 1240)
            spacing: 22

            PageHead {
                eyebrow: "Saber do time"
                title: "Skills"
                sub: "Jeitos de fazer que os funcionários seguem. Cada um vê o nome e a descrição; quando um pedido casa, lê os passos. No chat, peça “guarda isso como skill” e aprove."
            }

            Row {
                spacing: 10
                PrimaryButton {
                    text: "Nova skill"
                    icon: "plus"
                    onClicked: editor.open(null, false)
                }
                GhostButton {
                    text: "Colar SKILL.md"
                    icon: "paste"
                    onClicked: editor.open(null, true)
                }
            }

            ErrorBanner {
                width: parent.width
                text: page.error
            }

            Repeater {
                model: page.sections
                delegate: Column {
                    id: sec
                    required property var modelData
                    width: col.width
                    spacing: 10
                    visible: modelData.list.length > 0 || modelData.id === ""
                    Row {
                        spacing: 8
                        Rectangle {
                            visible: sec.modelData.color !== ""
                            width: 8
                            height: 8
                            radius: 3
                            color: sec.modelData.color || "transparent"
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Overline { leftPadding: 4;
                            text: (sec.modelData.id === "" ? "TODO O TIME" : "PROJETO · " + sec.modelData.name.toUpperCase()) + "  ·  " + sec.modelData.list.length
                        }
                    }
                    Text {
                        visible: sec.modelData.list.length === 0
                        leftPadding: 4
                        text: "Nenhuma skill ainda. Comece por uma das prontas abaixo, ou ensine no chat."
                        color: Theme.text3
                        font.pixelSize: Theme.fontBody
                    }
                    Repeater {
                        model: sec.modelData.list
                        delegate: Rectangle {
                            id: row
                            required property var modelData
                            width: col.width
                            height: 64
                            radius: Theme.radiusLg
                            color: hov.hovered ? Theme.hover : Theme.wash1
                            border.width: 1
                            border.color: Theme.line
                            Behavior on color { ColorAnimation { duration: Theme.fast } }
                            HoverHandler { id: hov; cursorShape: Qt.PointingHandCursor }
                            TapHandler { onTapped: editor.open(row.modelData, false) }
                            Rectangle {
                                id: badge
                                x: 14
                                anchors.verticalCenter: parent.verticalCenter
                                width: 36
                                height: 36
                                radius: Theme.radiusMd
                                color: Theme.hover
                                border.width: 1
                                border.color: Theme.line
                                Icon {
                                    anchors.centerIn: parent
                                    name: "book"
                                    size: 17
                                    color: Theme.text2
                                }
                            }
                            Column {
                                anchors { left: badge.right; leftMargin: 14; right: by.left; rightMargin: 14; verticalCenter: parent.verticalCenter }
                                spacing: 4
                                Text {
                                    text: row.modelData.name
                                    color: Theme.white
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontLarge
                                }
                                Text {
                                    width: parent.width
                                    text: row.modelData.description
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontBody
                                    elide: Text.ElideRight
                                }
                            }
                            Text {
                                id: by
                                anchors { right: parent.right; rightMargin: 18; verticalCenter: parent.verticalCenter }
                                text: "por " + page.author(row.modelData.author)
                                color: Theme.text4
                                font.pixelSize: Theme.fontSmall
                            }
                        }
                    }
                }
            }

            // Skills that come with Avora, added with one click.
            Column {
                visible: page.missingStarters.length > 0
                width: parent.width
                spacing: 12
                Overline { leftPadding: 4; text: "PRONTAS PARA ADICIONAR" }
                Flow {
                    id: starterGrid
                    width: parent.width
                    spacing: 14
                    readonly property real cell: {
                        const n = Math.max(1, Math.floor((width + spacing) / (300 + spacing)));
                        return (width - (n - 1) * spacing) / n;
                    }
                    Repeater {
                        model: page.missingStarters
                        delegate: Card {
                            id: st
                            required property var modelData
                            width: starterGrid.cell
                            height: 132
                            padding: 18
                            Column {
                                width: parent.width
                                spacing: 6
                                Text {
                                    text: st.modelData.name
                                    color: Theme.white
                                    font.family: Theme.mono
                                    font.pixelSize: Theme.fontLarge
                                }
                                Text {
                                    width: parent.width
                                    height: 36
                                    text: st.modelData.description
                                    color: Theme.text3
                                    font.pixelSize: Theme.fontBody
                                    wrapMode: Text.Wrap
                                    elide: Text.ElideRight
                                    maximumLineCount: 2
                                }
                            }
                            GhostButton {
                                anchors { left: parent.left; bottom: parent.bottom }
                                small: true
                                icon: "plus"
                                text: page.busy === st.modelData.id ? "Adicionando…" : "Adicionar"
                                onClicked: page.addStarter(st.modelData)
                            }
                        }
                    }
                }
            }

            // Programs on the nodes that skills use through the ferramenta tool.
            Column {
                visible: page.tools.length > 0
                width: parent.width
                spacing: 12
                Overline { leftPadding: 4; text: "FERRAMENTAS NOS NÓS" }
                Text {
                    width: Math.min(parent.width, 680)
                    leftPadding: 4
                    text: "Programas que os funcionários rodam por ações fixas, sem shell, na pasta do projeto ou na deles. Instale no PC ou na VPS com o comando que aparece e, em até 10 minutos, ela fica disponível (no PC, reabra o Avora)."
                    color: Theme.text3
                    font.pixelSize: Theme.fontBody
                    wrapMode: Text.Wrap
                }
                Card {
                    width: parent.width
                    height: toolCol.implicitHeight + 2 * padding
                    padding: 8
                    Column {
                        id: toolCol
                        width: parent.width
                        Repeater {
                            model: page.tools
                            delegate: Item {
                                id: tl
                                required property var modelData
                                required property int index
                                readonly property var online: modelData.nodes.filter(n => n.online)
                                readonly property var missing: online.filter(n => !n.has)
                                width: toolCol.width
                                height: Math.max(58, tlText.implicitHeight + 24)
                                Rectangle {
                                    visible: tl.index > 0
                                    width: parent.width - 24
                                    x: 12
                                    height: 1
                                    color: Theme.line
                                }
                                Column {
                                    id: tlText
                                    anchors { left: parent.left; leftMargin: 14; right: places.left; rightMargin: 14; verticalCenter: parent.verticalCenter }
                                    spacing: 3
                                    Text {
                                        text: tl.modelData.label + "   <font color='" + Theme.text3 + "'>" + tl.modelData.desc + "</font>"
                                        textFormat: Text.StyledText
                                        color: Theme.text
                                        font.pixelSize: Theme.fontLarge
                                        width: parent.width
                                        elide: Text.ElideRight
                                    }
                                    Repeater {
                                        model: tl.missing
                                        delegate: TextEdit {
                                            required property var modelData
                                            readOnly: true
                                            selectByMouse: true
                                            width: tlText.width
                                            wrapMode: TextEdit.Wrap
                                            text: (modelData.kind === "vps" ? "VPS: " : "PC: ") + (tl.modelData.install[modelData.kind === "vps" ? "linux" : "windows"] || "")
                                            color: Theme.text4
                                            selectionColor: Theme.lineStrong
                                            font.family: Theme.mono
                                            font.pixelSize: Theme.fontCaption
                                        }
                                    }
                                }
                                Row {
                                    id: places
                                    anchors { right: parent.right; rightMargin: 14; verticalCenter: parent.verticalCenter }
                                    spacing: 6
                                    Repeater {
                                        model: tl.online
                                        delegate: Rectangle {
                                            id: pl
                                            required property var modelData
                                            implicitHeight: 24
                                            implicitWidth: plText.implicitWidth + 26
                                            radius: Theme.radiusSm
                                            color: modelData.has ? Qt.rgba(0.53, 0.89, 0.74, 0.08) : Theme.wash1
                                            border.width: 1
                                            border.color: modelData.has ? Qt.rgba(0.53, 0.89, 0.74, 0.25) : Theme.line
                                            Rectangle {
                                                x: 9
                                                anchors.verticalCenter: parent.verticalCenter
                                                width: 5
                                                height: 5
                                                radius: 3
                                                color: pl.modelData.has ? Theme.ok : Theme.text4
                                            }
                                            Text {
                                                id: plText
                                                x: 19
                                                anchors.verticalCenter: parent.verticalCenter
                                                text: (pl.modelData.kind === "vps" ? "VPS" : "PC") + (!pl.modelData.has ? " · falta" : plHov.hovered ? " · " + pl.modelData.version : "")
                                                color: pl.modelData.has ? Theme.text : Theme.text4
                                                font.pixelSize: Theme.fontSmall
                                            }
                                            HoverHandler { id: plHov }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    SkillEditor {
        id: editor
        anchors.fill: parent
        projects: page.projects
        onChanged: page.load()
    }
}
