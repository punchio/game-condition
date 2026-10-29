from pathlib import Path
import json

ROOT = Path(__file__).resolve().parent
W, H = 1920, 1280
FONT = {"latin": "Liter", "ea": "MiSans"}
elements = []
serial = 0


def eid(prefix):
    global serial
    serial += 1
    return f"{prefix}-{serial}"


def fill(color):
    return {"type": "solid", "color": color}


def shape(x, y, w, h, color=None, border=None, prefix="shape"):
    item = {
        "elementId": eid(prefix),
        "elementType": "shape",
        "bounds": [x, y, w, h],
        "shapeName": "rect",
    }
    if color:
        item["fill"] = fill(color)
    if border:
        item["border"] = border
    elements.append(item)


def text(x, y, w, h, value, size=14, color="$ink", bold=False,
         align="left", valign="middle", wrap=False, prefix="text", line_height=1.15):
    elements.append({
        "elementId": eid(prefix),
        "elementType": "text",
        "bounds": [x, y, w, h],
        "content": {
            "text": value,
            "fontSize": size,
            "fontFamily": FONT,
            "color": color,
            "bold": bold,
            "lineHeight": line_height,
            "align": [align, valign],
            "wrap": wrap,
        },
    })


def polyline(x, y, w, h, points, color="$teal", width=1.5, dashed=False,
             arrow_end=True, prefix="line"):
    elements.append({
        "elementId": eid(prefix),
        "elementType": "line",
        "bounds": [x, y, w, h],
        "viewBox": [w, h],
        "points": points,
        "curve": "sharp",
        "arrow": [None, "stealth"] if arrow_end else [None, None],
        "border": {
            "style": "dash" if dashed else "solid",
            "width": width,
            "color": color,
        },
    })


def hline(x1, x2, y, color="$line", width=1, dashed=False, arrow_end=False):
    left, distance = min(x1, x2), abs(x2 - x1)
    points = f"0,1 {distance},1" if x2 >= x1 else f"{distance},1 0,1"
    polyline(left, y - 1, distance, 2, points, color, width, dashed, arrow_end)


def vline(x, y1, y2, color="$line", width=1, dashed=True):
    polyline(x - 1, y1, 2, y2 - y1, f"1,0 1,{y2-y1}", color, width, dashed, False)


def message(x1, x2, y, label, dashed=False, color="$teal", size=13,
            label_x=None, label_w=None):
    hline(x1, x2, y, color, 1.6, dashed, True)
    if label_w is None:
        label_w = max(90, abs(x2 - x1) - 24)
    if label_x is None:
        label_x = (x1 + x2 - label_w) / 2
    text(label_x, y - 31, label_w, 26, label, size, "$ink", False,
         "center", "middle", False, "message-label")


def phase(y, number, title, tag):
    shape(48, y, 30, 30, "$ink", prefix="phase-number-bg")
    text(48, y, 30, 30, number, 13, "#FFFFFF", True, "center", prefix="phase-number")
    text(88, y - 1, 140, 30, title, 15, "$ink", True, prefix="phase-title")
    text(88, y + 29, 145, 24, tag, 11, "$muted", prefix="phase-tag")


def self_loop(x, y, label, label_w, color="$teal"):
    polyline(x, y, 66, 34, "0,0 66,0 66,34 0,34", color, 1.4, False, True)
    text(x + 76, y - 2, label_w, 30, label, 12, "$ink", prefix="self-label")


# Background, frames and phase rules.
shape(0, 0, W, H, "$bg", prefix="canvas")
for y in (245, 425, 555, 710, 910, 1100):
    hline(42, 1878, y, "$line", 1)
shape(232, 425, 1648, 655, None, {"style": "solid", "width": 1, "color": "$frame"}, "plan-loop")
text(248, 429, 180, 24, "loop　候选计划", 12, "$muted", True, prefix="loop-label")
shape(232, 710, 1648, 370, None, {"style": "solid", "width": 1.4, "color": "$teal"}, "instance-loop")
text(248, 714, 300, 24, "loop　活跃实例（按注册顺序）", 12, "$teal", True, prefix="loop-label")

centers = {
    "biz": 280,
    "manager": 500,
    "deps": 720,
    "plan": 940,
    "state": 1160,
    "counter": 1380,
    "eval": 1600,
    "handler": 1820,
}
participants = [
    ("biz", "业务调用方", "ChangeSet"),
    ("manager", "Manager", "doTrigger"),
    ("deps", "依赖索引", "deps / depRef"),
    ("plan", "plan", "候选粗筛 / 事件预算"),
    ("state", "共享状态槽", "sourceState / StateSource"),
    ("counter", "Counter", "Update / Read"),
    ("eval", "evalPlan", "Match / Progress"),
    ("handler", "Handler", "可选"),
]
for key, _, _ in participants:
    vline(centers[key], 228, 1185, "$lifeline", 1, True)

# Participant headers sit above lifelines.
for key, title, subtitle in participants:
    width = 190 if key != "handler" else 180
    x = centers[key] - width / 2
    header_color = "$teal" if key == "manager" else "$header"
    title_color = "#FFFFFF" if key == "manager" else "$ink"
    sub_color = "#D8EEE7" if key == "manager" else "$muted"
    shape(x, 170, width, 58, header_color, prefix="participant-bg")
    shape(x, 170, width, 3, "$amber" if key == "state" else "$teal", prefix="participant-accent")
    text(x + 5, 178, width - 10, 24, title, 15, title_color, True, "center", prefix="participant-title")
    text(x + 5, 201, width - 10, 19, subtitle, 10.5, sub_color, False, "center", prefix="participant-subtitle")

# Title and legend.
text(48, 38, 800, 48, "Trigger 运行时序", 34, "$ink", True, prefix="title")
text(50, 91, 980, 28, "条件系统 · 状态标脏、事件增量与实例求值", 16, "$muted", prefix="subtitle")
text(1450, 48, 420, 24, "CURRENT IMPLEMENTATION", 12, "$teal", True, "right", prefix="eyebrow")
text(1450, 77, 420, 24, "六步处理 · 两级循环", 13, "$muted", False, "right", prefix="meta")

# Phase labels.
phase(258, "01", "收集标脏", "round / collect")
phase(445, "02", "候选粗筛", "计划级")
phase(570, "03", "事件增量", "计划级 · 一次")
phase(738, "04", "own / Counter", "实例级")
phase(928, "05", "求值回调", "实例级 · 独立 Progress")
phase(1112, "06", "重入清理", "队列 / 墓碑")

# The own-only branch occupies otherwise unused lanes in step 1.
shape(1240, 267, 590, 112, "$amberLight", prefix="own-note-bg")
shape(1240, 267, 5, 112, "$amber", prefix="own-note-accent")
text(1262, 276, 540, 26, "own-only 分支", 14, "$amber", True, prefix="own-note-title")
text(1262, 307, 540, 56,
     "slot.dirty = true；不加入 affected\n不回调、不求值、不更新 Counter",
     13, "$ink", False, "left", "top", True, "own-note-body", 1.3)

# Step 1: collect and dirty state slots.
message(centers["biz"], centers["manager"], 280, "Trigger(cs)，round++")
message(centers["manager"], centers["deps"], 323, "collect(keys)")
message(centers["deps"], centers["manager"], 355, "depRef", True)
message(centers["manager"], centers["state"], 390, "普通状态 / own 依赖：标脏查询槽", False,
        "$teal", 13, 650, 400)

# Step 2: candidate filtering.
message(centers["manager"], centers["plan"], 490, "activeCount + eventCandidate")
message(centers["plan"], centers["manager"], 525, "保留或跳过", True)

# Step 3: plan-level event delta.
message(centers["manager"], centers["plan"], 605, "collectDelta")
self_loop(centers["plan"], 628, "过滤 + 聚合（每计划一次）", 280)
message(centers["plan"], centers["manager"], 680, "pendingDelta", True)

# Step 4: instance binding, own and Counter.
self_loop(centers["manager"], 755, "绑定当前 key / plan", 190)
message(centers["manager"], centers["state"], 805, "own 读取共享状态槽", False,
        "$teal", 13, 650, 400)
message(centers["state"], centers["manager"], 838, "value / has（dirty 时 Eval）", True,
        "$muted", 13, 650, 400)
message(centers["manager"], centers["counter"], 875, "own 满足才 Update", False,
        "$teal", 13, 690, 360)

# Step 5: independent Progress and optional Handler.
message(centers["manager"], centers["eval"], 950, "evalPlan(p, ctx)")
text(1500, 965, 200, 24, "每次新建 Progress", 12, "$teal", True, "center", prefix="progress-note")
message(centers["eval"], centers["manager"], 1005, "match, progress", True, "$muted")
message(centers["manager"], centers["handler"], 1040, "Handler(Result)")
message(centers["handler"], centers["manager"], 1070, "true → cancel / Del", True, "$muted")

# Step 6: queued commands and tombstone cleanup.
self_loop(centers["manager"], 1135, "队列命令继续执行；最终 sweepDirty", 330)
message(centers["manager"], centers["biz"], 1180, "Trigger 返回", True, "$muted")

# Footer legend and reading note.
hline(48, 100, 1230, "$teal", 1.6, False, True)
text(112, 1216, 100, 28, "调用", 12, "$muted")
hline(190, 242, 1230, "$muted", 1.4, True, True)
text(254, 1216, 100, 28, "返回", 12, "$muted")
shape(340, 1222, 12, 12, "$amber", prefix="legend-amber")
text(362, 1216, 230, 28, "own-only 提前结束", 12, "$muted")
text(1150, 1216, 720, 28, "同一轮共享状态；Progress 每次求值独立分配", 13,
     "$ink", True, "right", prefix="footer-note")

page = {
    "pageType": "content",
    "background": fill("$bg"),
    "elements": elements,
}
main = {
    "version": "v2",
    "title": "Trigger 运行时序",
    "size": [W, H],
    "customFonts": [
        {"family": "MiSans", "src": "https://statics.kimi.ai/neo-design-static/p-font/MiSans.ttf"},
        {"family": "Liter", "src": "https://statics.kimi.ai/neo-design-static/p-font/Liter.ttf"},
    ],
    "theme": {
        "colors": {
            "bg": "#F7F5F0",
            "ink": "#18231F",
            "muted": "#68736D",
            "teal": "#176B57",
            "amber": "#A96A12",
            "amberLight": "#F3E7CF",
            "header": "#E8ECE7",
            "line": "#D8DCD5",
            "frame": "#AAB4AD",
            "lifeline": "#C8CEC8",
        },
    },
    "pages": ["pages/1_trigger.page"],
}


def scalar(value):
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, (int, float)):
        return str(value)
    return json.dumps(value, ensure_ascii=False)


def dump_yaml(value, indent=0):
    pad = " " * indent
    lines = []
    if isinstance(value, dict):
        for key, item in value.items():
            if isinstance(item, (dict, list)):
                lines.append(f"{pad}{key}:")
                lines.extend(dump_yaml(item, indent + 2))
            else:
                lines.append(f"{pad}{key}: {scalar(item)}")
    elif isinstance(value, list):
        for item in value:
            if isinstance(item, (dict, list)):
                lines.append(f"{pad}-")
                lines.extend(dump_yaml(item, indent + 2))
            else:
                lines.append(f"{pad}- {scalar(item)}")
    else:
        lines.append(pad + scalar(value))
    return lines


def write_yaml(path, value):
    try:
        import yaml
        content = yaml.safe_dump(value, allow_unicode=True, sort_keys=False, width=140)
    except ImportError:
        content = "\n".join(dump_yaml(value)) + "\n"
    path.write_text(content, encoding="utf-8")


(ROOT / "pages").mkdir(parents=True, exist_ok=True)
write_yaml(ROOT / "trigger-sequence.kanva.pptd", main)
write_yaml(ROOT / "pages" / "1_trigger.page", page)
print(f"generated {len(elements)} editable elements")
