"""StatusNotifierItem·dbusmenu 클라이언트(트레이 검증용): 메뉴 목록 출력, 라벨로 항목 클릭, 툴팁 출력."""

import sys
import time

import dbus

bus = dbus.SessionBus()
dest, cmd = sys.argv[1], sys.argv[2]


def layout():
    menu = bus.get_object(dest, "/StatusNotifierItem/menu")
    _, root = menu.GetLayout(
        0, -1, dbus.Array([], signature="s"), dbus_interface="com.canonical.dbusmenu"
    )
    items = []
    for child in root[2]:
        cid, props = int(child[0]), child[1]
        items.append(
            (
                cid,
                str(props.get("label", "")),
                bool(props.get("enabled", True)),
                str(props.get("type", "")),
            )
        )
    return items


if cmd == "layout":
    for cid, label, enabled, typ in layout():
        print(f"{cid}\t{'on' if enabled else 'off'}\t{typ or 'item'}\t{label}")
elif cmd == "click":
    want = sys.argv[3]
    for cid, label, enabled, _ in layout():
        if label.startswith(want):
            menu = bus.get_object(dest, "/StatusNotifierItem/menu")
            menu.Event(
                cid,
                "clicked",
                dbus.String("", variant_level=1),
                dbus.UInt32(int(time.time())),
                dbus_interface="com.canonical.dbusmenu",
            )
            print(f"clicked {cid} {label}")
            break
    else:
        sys.exit("no item " + want)
elif cmd == "tooltip":
    item = bus.get_object(dest, "/StatusNotifierItem")
    tip = item.Get(
        "org.kde.StatusNotifierItem",
        "ToolTip",
        dbus_interface="org.freedesktop.DBus.Properties",
    )
    print("tooltip:", str(tip[2]), "|", str(tip[3]))
    icon = item.Get(
        "org.kde.StatusNotifierItem",
        "IconPixmap",
        dbus_interface="org.freedesktop.DBus.Properties",
    )
    print("icon pixmaps:", [(int(w), int(h), len(b)) for w, h, b in icon])
