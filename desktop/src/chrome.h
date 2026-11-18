#pragma once

class QWindow;

// applyChrome gives the frameless window what Windows normally draws around a
// framed one: the soft drop shadow, rounded corners on Windows 11 and a dark
// border. The window itself paints the title bar and controls in QML.
void applyChrome(QWindow *window);
