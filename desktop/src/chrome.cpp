#include "chrome.h"

#include <QWindow>

#include <windows.h>
#include <dwmapi.h>

// Values from the Windows 11 SDK; spelled out so older SDK headers still build.
namespace {
constexpr DWORD kUseImmersiveDarkMode = 20; // DWMWA_USE_IMMERSIVE_DARK_MODE
constexpr DWORD kCornerPreference = 33;     // DWMWA_WINDOW_CORNER_PREFERENCE
constexpr DWORD kBorderColor = 34;          // DWMWA_BORDER_COLOR
constexpr int kCornerRound = 2;             // DWMWCP_ROUND
} // namespace

void applyChrome(QWindow *window)
{
    const HWND hwnd = reinterpret_cast<HWND>(window->winId());

    // A popup window only gets the DWM shadow when non-client rendering is on
    // and the frame reaches into the client area by at least a pixel.
    const DWMNCRENDERINGPOLICY policy = DWMNCRP_ENABLED;
    DwmSetWindowAttribute(hwnd, DWMWA_NCRENDERING_POLICY, &policy, sizeof policy);
    const MARGINS margins{1, 1, 1, 1};
    DwmExtendFrameIntoClientArea(hwnd, &margins);

    const BOOL dark = TRUE;
    DwmSetWindowAttribute(hwnd, kUseImmersiveDarkMode, &dark, sizeof dark);
    const int corner = kCornerRound;
    DwmSetWindowAttribute(hwnd, kCornerPreference, &corner, sizeof corner);
    const COLORREF border = RGB(0x24, 0x24, 0x29);
    DwmSetWindowAttribute(hwnd, kBorderColor, &border, sizeof border);
}
