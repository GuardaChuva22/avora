pragma Singleton
import QtQuick

// Design tokens. "Vidro refinado": near-black, cool greys, white as the
// only accent. Surfaces are flat (no gradients); light is earned: a glow
// only marks focus or the selected thing, never something at rest.
//
// Every screen takes its sizes from here: no literal font size, radius or
// white wash in the QML. Need a new one? Add a step to the scale, don't
// invent a value in place.
QtObject {
    readonly property color bg: "#050506"
    readonly property color panel: Qt.rgba(0, 0, 0, 0.38)
    readonly property color card: "#0d0d10"
    readonly property color cardHover: "#121216"
    readonly property color cardTop: card     // kept for older callers
    readonly property color cardBottom: card
    readonly property color well: Qt.rgba(0, 0, 0, 0.45)
    readonly property color line: Qt.rgba(1, 1, 1, 0.08)
    readonly property color lineStrong: Qt.rgba(1, 1, 1, 0.15)
    readonly property color focusLine: Qt.rgba(1, 1, 1, 0.30) // edge of the focused field/button
    readonly property real glow: 0.10                         // white aura on focus/selection
    readonly property color hover: Qt.rgba(1, 1, 1, 0.045)
    readonly property color active: Qt.rgba(1, 1, 1, 0.075)

    // White washes, faintest to strongest: fills on the dark surfaces.
    readonly property color wash1: Qt.rgba(1, 1, 1, 0.03)  // a faint well, a skeleton
    readonly property color wash2: hover                   // hover
    readonly property color wash3: active                  // pressed, selected row
    readonly property color wash4: Qt.rgba(1, 1, 1, 0.10)  // chip on, badge
    readonly property color wash5: Qt.rgba(1, 1, 1, 0.16)  // strong border, track
    readonly property color wash6: Qt.rgba(1, 1, 1, 0.25)  // handle, divider dot
    readonly property color scrim: Qt.rgba(0, 0, 0, 0.6)   // behind sheets and over images

    readonly property color text: "#ededf0"
    readonly property color text2: "#b8b8c0"
    readonly property color text3: "#80808a"
    readonly property color text4: "#56565f"
    readonly property color white: "#ffffff"
    readonly property color ink: "#050506"

    readonly property color ok: "#86e3bd"
    readonly property color live: "#2eff89" // pulsing "on / connected" lights
    readonly property color bad: "#ff8a8a"
    readonly property color warn: "#f2cd82"
    readonly property color badWash: Qt.rgba(1, 0.54, 0.54, 0.08) // fill of an error or danger
    readonly property color badLine: Qt.rgba(1, 0.54, 0.54, 0.22) // its border

    // The colors an agent or a project can wear.
    readonly property var palette: ["#c9b8ff", "#7fd1ff", "#ffc48a", "#86e3bd", "#ff9db8", "#f2cd82", "#9fb4ff", "#e8e8ee"]

    readonly property string sans: "Segoe UI Variable Text"
    readonly property string display: "Segoe UI Variable Display"
    readonly property string mono: Qt.fontFamilies().indexOf("Cascadia Mono") >= 0 ? "Cascadia Mono" : "Consolas"

    // Type scale (px). Caps labels use caption + DemiBold + trackCaps.
    readonly property int fontTiny: 10     // badges, counters
    readonly property int fontCaption: 11  // caps labels, timestamps
    readonly property int fontSmall: 12    // secondary text, hints
    readonly property int fontBody: 13     // body, list rows, fields
    readonly property int fontLarge: 14    // card titles, page subtitles
    readonly property int fontTitle: 16    // sheet and panel titles
    readonly property int fontHeading: 20  // section headings
    readonly property int fontDisplay: 24  // empty-state titles
    readonly property int fontHero: 32     // page titles
    readonly property real trackCaps: 1.6  // letter spacing of caps labels

    // Corner radii. Pills and circles use height / 2.
    readonly property int radiusXs: 4   // bars, tiny marks
    readonly property int radiusSm: 8   // chips, small buttons, thumbnails
    readonly property int radiusMd: 12  // fields, buttons, rows
    readonly property int radiusLg: 14  // inner cards, menus
    readonly property int radius: 18    // cards, sheets, panels
    readonly property int radiusXl: radius

    // Spacing steps (px) for new layouts.
    readonly property int space1: 4
    readonly property int space2: 8
    readonly property int space3: 12
    readonly property int space4: 16
    readonly property int space5: 24
    readonly property int space6: 32
    readonly property int gutter: 36
    readonly property int titleBar: 40

    // Motion. One feel for the whole app: hover and press answer in ~100 ms,
    // state changes in ~180 ms, things that travel (panels, the sidebar)
    // in ~220-280 ms; everything decelerates (OutCubic), nothing bounces.
    readonly property int fast: 100   // hover, press, color
    readonly property int medium: 180 // fades, state changes
    readonly property int move: 220   // sizes and positions (sidebar, thumbs)
    readonly property int slow: 280   // a panel or sheet arriving
    readonly property int ease: Easing.OutCubic
}
