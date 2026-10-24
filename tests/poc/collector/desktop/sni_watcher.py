"""최소 StatusNotifierWatcher(KDE SNI 사양): 트레이 검증용. 등록된 항목의 버스 이름을 파일에 남긴다."""

import sys

import dbus
import dbus.service
from dbus.mainloop.glib import DBusGMainLoop
from gi.repository import GLib

OUT = sys.argv[1]


class Watcher(dbus.service.Object):
    def __init__(self, bus):
        self.items = []
        name = dbus.service.BusName("org.kde.StatusNotifierWatcher", bus)
        super().__init__(name, "/StatusNotifierWatcher")

    @dbus.service.method(
        "org.kde.StatusNotifierWatcher",
        in_signature="s",
        out_signature="",
        sender_keyword="sender",
    )
    def RegisterStatusNotifierItem(self, service, sender=None):
        self.items.append((sender, service))
        with open(OUT, "a") as f:
            f.write(f"{sender} {service}\n")

    @dbus.service.method(
        "org.kde.StatusNotifierWatcher", in_signature="s", out_signature=""
    )
    def RegisterStatusNotifierHost(self, service):
        pass


DBusGMainLoop(set_as_default=True)
w = Watcher(dbus.SessionBus())
GLib.MainLoop().run()
