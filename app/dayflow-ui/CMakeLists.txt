cmake_minimum_required(VERSION 3.21)
project(dayflow-ui VERSION 1.6.0 LANGUAGES CXX)

# dayflow-ui — standalone Qt6/QML host for the Dayflow tab QML (plan U4/U5).
# The tab QML at the repo root is embedded as-is into the Dayflow.App module;
# Omarchy/Quickshell-only types it imports (qs.Commons, qs.Ui, Quickshell,
# Quickshell.Io) are provided by the shim modules in this directory.

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)

find_package(Qt6 6.5 REQUIRED COMPONENTS Core Gui Qml Quick Widgets)
qt_standard_project_setup(REQUIRES 6.5)
# QTP0004 NEW: each QML module gets its own resource dir.
if(QT_KNOWN_POLICY_QTP0004)
  qt_policy(SET QTP0004 NEW)
endif()

set(DAYFLOW_REPO_ROOT "${CMAKE_CURRENT_SOURCE_DIR}/../.." CACHE PATH "Dayflow repo root (tab QML lives here)")
get_filename_component(DAYFLOW_REPO_ROOT "${DAYFLOW_REPO_ROOT}" ABSOLUTE)

# ---- Quickshell.Io shim: Process / StdioCollector / SplitParser -------------
qt_add_library(dayflow_io STATIC)
qt_add_qml_module(dayflow_io
  URI Quickshell.Io
  OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/qmlmods/Quickshell/Io
  VERSION 1.0
  SOURCES process.h process.cpp
)
target_link_libraries(dayflow_io PUBLIC Qt6::Core Qt6::Gui Qt6::Qml)

# ---- Quickshell shim: iconPath() singleton + FloatingWindow ------------------
qt_add_library(dayflow_qs STATIC)
qt_add_qml_module(dayflow_qs
  URI Quickshell
  OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/qmlmods/Quickshell
  VERSION 1.0
  SOURCES quickshellshim.h quickshellshim.cpp
  QML_FILES qml/Quickshell/FloatingWindow.qml
)
target_link_libraries(dayflow_qs PUBLIC Qt6::Core Qt6::Gui Qt6::Qml Qt6::Quick)

# ---- qs.Commons (C++ Color + QML Style) / qs.Ui shims ------------------------
qt_add_library(dayflow_commons STATIC)
set_source_files_properties(qml/qs/Ui/HostSizing.qml PROPERTIES QT_QML_SINGLETON_TYPE TRUE)
set_source_files_properties(qml/qs/Commons/Style.qml PROPERTIES QT_QML_SINGLETON_TYPE TRUE)
qt_add_qml_module(dayflow_commons
  URI qs.Commons
  OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/qmlmods/qs/Commons
  VERSION 1.0
  SOURCES theme.h
  QML_FILES qml/qs/Commons/Style.qml
)

qt_add_library(dayflow_ui_shim STATIC)
qt_add_qml_module(dayflow_ui_shim
  URI qs.Ui
  OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/qmlmods/qs/Ui
  VERSION 1.0
  QML_FILES
    qml/qs/Ui/Panel.qml
    qml/qs/Ui/KeyboardPanel.qml
    qml/qs/Ui/PanelKeyCatcher.qml
    qml/qs/Ui/PanelSeparator.qml
    qml/qs/Ui/PanelController.qml
    qml/qs/Ui/HostSizing.qml
)
target_link_libraries(dayflow_commons PUBLIC Qt6::Core Qt6::Gui Qt6::Qml)
target_link_libraries(dayflow_ui_shim PRIVATE dayflow_commons)

# ---- Dayflow.App: the unmodified tab QML from the repo root -------------------
# BarWidget.qml is the Omarchy bar button; it has no place in a window host.
file(GLOB DAYFLOW_TAB_QML CONFIGURE_DEPENDS "${DAYFLOW_REPO_ROOT}/*.qml")
list(REMOVE_ITEM DAYFLOW_TAB_QML "${DAYFLOW_REPO_ROOT}/BarWidget.qml")
foreach(f IN LISTS DAYFLOW_TAB_QML)
  get_filename_component(n "${f}" NAME)
  set_source_files_properties("${f}" PROPERTIES QT_RESOURCE_ALIAS "${n}")
endforeach()
qt_add_library(dayflow_app STATIC)
qt_add_qml_module(dayflow_app
  URI Dayflow.App
  OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/qmlmods/Dayflow/App
  VERSION 1.0
  QML_FILES ${DAYFLOW_TAB_QML}
  NO_CACHEGEN
)
target_link_libraries(dayflow_app PRIVATE dayflow_commons dayflow_ui_shim dayflow_qs dayflow_io)

# ---- executable ---------------------------------------------------------------
qt_add_executable(dayflow-ui main.cpp tray.h tray.cpp enginepath.h enginepath.cpp)
qt_add_qml_module(dayflow-ui
  URI Dayflow.Host
  OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/qmlmods/Dayflow/Host
  VERSION 1.0
  QML_FILES qml/host/Main.qml
)
target_compile_definitions(dayflow-ui PRIVATE DAYFLOW_UI_VERSION="${PROJECT_VERSION}")
target_link_libraries(dayflow-ui PRIVATE
  Qt6::Core Qt6::Gui Qt6::Qml Qt6::Quick Qt6::Widgets
  dayflow_io dayflow_ioplugin dayflow_qs dayflow_qsplugin
  dayflow_commons dayflow_commonsplugin dayflow_ui_shim dayflow_ui_shimplugin
  dayflow_app dayflow_appplugin
)

# ---- bundled engine (KTD5): build dayflow beside dayflow-ui -------------------
find_program(GO_EXECUTABLE go)
option(DAYFLOW_BUILD_ENGINE "Build the Go engine next to dayflow-ui" ON)
if(DAYFLOW_BUILD_ENGINE AND GO_EXECUTABLE)
  add_custom_target(dayflow-engine ALL
    COMMAND ${CMAKE_COMMAND} -E env CGO_ENABLED=0 ${GO_EXECUTABLE} build -trimpath
            -o ${CMAKE_CURRENT_BINARY_DIR}/dayflow .
    WORKING_DIRECTORY ${DAYFLOW_REPO_ROOT}/engine
    COMMENT "Building bundled dayflow engine"
    VERBATIM)
  add_dependencies(dayflow-ui dayflow-engine)
endif()

# ---- install: binaries, desktop entry, icon, autostart template ----------------
# Default prefix is the install-app.sh one; packages pass their own. The desktop
# entries carry absolute Exec paths under the configure-time prefix so a launcher
# that does not have ~/.local/bin on PATH still finds the app.
include(GNUInstallDirs)
set(DAYFLOW_UI_EXEC "${CMAKE_INSTALL_FULL_BINDIR}/dayflow-ui")
set(DAYFLOW_ENGINE_EXEC "${CMAKE_INSTALL_FULL_BINDIR}/dayflow")
configure_file(dist/dayflow.desktop.in ${CMAKE_CURRENT_BINARY_DIR}/dayflow.desktop @ONLY)
configure_file(dist/dayflow-autostart.desktop.in ${CMAKE_CURRENT_BINARY_DIR}/dayflow-autostart.desktop @ONLY)
install(TARGETS dayflow-ui RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
# The engine beside dayflow-ui (KTD5). Skipped when Go was not found at build time.
install(PROGRAMS ${CMAKE_CURRENT_BINARY_DIR}/dayflow DESTINATION ${CMAKE_INSTALL_BINDIR} OPTIONAL)
install(FILES ${CMAKE_CURRENT_BINARY_DIR}/dayflow.desktop DESTINATION ${CMAKE_INSTALL_DATADIR}/applications)
install(FILES dist/dayflow.svg DESTINATION ${CMAKE_INSTALL_DATADIR}/icons/hicolor/scalable/apps)
# XDG autostart lives outside the prefix (~/.config/autostart), so the template is
# installed under share/dayflow and install-app.sh --autostart copies it.
install(FILES ${CMAKE_CURRENT_BINARY_DIR}/dayflow-autostart.desktop
  DESTINATION ${CMAKE_INSTALL_DATADIR}/dayflow/autostart RENAME dayflow.desktop)

# ---- tests --------------------------------------------------------------------
enable_testing()
add_test(NAME offscreen-smoke
  COMMAND ${CMAKE_CURRENT_SOURCE_DIR}/tests/smoke.sh $<TARGET_FILE:dayflow-ui>)
# Every rail section loads with seeded data and no QML errors (needs the engine).
if(DAYFLOW_BUILD_ENGINE AND GO_EXECUTABLE)
  add_test(NAME sections
    COMMAND ${CMAKE_CURRENT_SOURCE_DIR}/tests/sections.sh $<TARGET_FILE:dayflow-ui>)
endif()
if(DAYFLOW_BUILD_ENGINE AND GO_EXECUTABLE)
  add_test(NAME nav
    COMMAND ${CMAKE_CURRENT_SOURCE_DIR}/tests/nav.sh $<TARGET_FILE:dayflow-ui>)
endif()
add_test(NAME install-layout
  COMMAND ${CMAKE_CURRENT_SOURCE_DIR}/tests/install-layout.sh ${CMAKE_BINARY_DIR})
