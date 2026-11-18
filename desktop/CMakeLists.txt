cmake_minimum_required(VERSION 3.21)
project(AvoraDesktop VERSION 0.1.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(Qt6 6.9 REQUIRED COMPONENTS Quick QuickControls2 Network Svg ShaderTools WebEngineQuick)
qt_standard_project_setup(REQUIRES 6.9)

qt_add_executable(avora-desktop WIN32
    src/main.cpp
    src/api.h src/api.cpp src/oauth.cpp src/folders.cpp
    src/tokenstore.h src/tokenstore.cpp
    src/chrome.h src/chrome.cpp
    src/avatar.h src/avatar.cpp
    src/serverimage.h src/serverimage.cpp
    src/webinput.h src/webinput.cpp
    src/pointerwatch.h src/pointerwatch.cpp
    src/icons.h src/icons.cpp
)
if(EXISTS ${CMAKE_CURRENT_SOURCE_DIR}/resources/avora.ico)
    target_sources(avora-desktop PRIVATE resources/avora.rc)
endif()

# QML sources live in qml/ but are served from the module root, next to its
# qmldir, so every file sees the module's types (and the Theme singleton).
file(GLOB AVORA_QML CONFIGURE_DEPENDS RELATIVE ${CMAKE_CURRENT_SOURCE_DIR} qml/*.qml qml/*.js)
foreach(f IN LISTS AVORA_QML)
    get_filename_component(name ${f} NAME)
    set_source_files_properties(${f} PROPERTIES QT_RESOURCE_ALIAS ${name})
endforeach()

set_source_files_properties(qml/Theme.qml qml/DocBus.qml PROPERTIES QT_QML_SINGLETON_TYPE TRUE)

qt_add_qml_module(avora-desktop
    URI Avora
    VERSION 1.0
    QML_FILES ${AVORA_QML}
    RESOURCES
        resources/icon.svg
        resources/logos/aliexpress.png
        resources/logos/google.png
        resources/logos/google-calendar.png
        resources/logos/google-contacts.png
        resources/logos/google-drive.png
        resources/logos/google-gmail.png
        resources/logos/google-sheets.png
        resources/logos/google-tasks.png
        resources/logos/google-youtube.png
        resources/logos/mercadolivre.png
        resources/logos/pix.png
        resources/logos/pluggy.png
        resources/logos/polymarket.png
)

qt_add_shaders(avora-desktop "shaders"
    PREFIX "/"
    FILES
        shaders/backdrop.frag
        shaders/orb.frag
)

target_include_directories(avora-desktop PRIVATE src)

target_link_libraries(avora-desktop PRIVATE
    Qt6::Quick Qt6::QuickControls2 Qt6::Network Qt6::Svg Qt6::WebEngineQuick
    dwmapi crypt32
)

if(MSVC)
    target_compile_options(avora-desktop PRIVATE /W4 /permissive- /utf-8)
endif()

install(TARGETS avora-desktop RUNTIME DESTINATION .)
