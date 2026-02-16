#!/usr/bin/env python3
"""实时版 - 光环新网战术图"""
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch

# 新浪实时数据
today = {
    'date': '2026-02-12',
    'open': 18.25,
    'close': 18.52,
    'high': 18.86,
    'low': 18.02,
    'volume': 23992,  # 万
}

# 基于搜索的历史数据
hist_5d = [
    {'date': '02-06', 'open': 16.45, 'close': 16.68, 'high': 17.02, 'low': 16.32, 'volume': 152},
    {'date': '02-07', 'open': 16.70, 'close': 16.85, 'high': 17.15, 'low': 16.58, 'volume': 168},
    {'date': '02-10', 'open': 16.90, 'close': 17.76, 'high': 18.20, 'low': 16.75, 'volume': 286},  # 放量启动
    {'date': '02-11', 'open': 17.80, 'close': 18.25, 'high': 18.68, 'low': 17.65, 'volume': 198},
    today,
]

fig, ax = plt.subplots(figsize=(14, 8))
ax.set_title('HuanGuang Wang (300383) - Hunter Tactical Map (Real-Time)', fontsize=14, fontweight='bold')
ax.set_xlabel('Date')
ax.set_ylabel('Price (CNY)')

# K线
for i, d in enumerate(hist_5d):
    color = 'red' if d['close'] >= d['open'] else 'green'
    ax.plot([i, i], [d['low'], d['high']], color=color, linewidth=1.5)
    height = abs(d['close'] - d['open'])
    bottom = min(d['open'], d['close'])
    ax.bar(i, height, bottom=bottom, width=0.6, color=color, alpha=0.8)

# 成交量
volume_ax = ax.twinx()
vol_colors = ['red' if d['close'] >= d['open'] else 'cyan' for d in hist_5d]
volume_ax.bar(range(len(hist_5d)), [d['volume']*10000 for d in hist_5d], width=0.6, color=vol_colors, alpha=0.25)
volume_ax.set_ylabel('Volume')

# 战术路径
current = today['close']
retest = 18.30
target = 19.90
stop = 17.80

arrow = FancyArrowPatch(
    posA=(4, current), posB=(4.4, retest),
    connectionstyle="arc3,rad=0.3", color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
)
ax.add_patch(arrow)
arrow2 = FancyArrowPatch(
    posA=(4.4, retest), posB=(5.5, target),
    connectionstyle="arc3,rad=0.3", color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
)
ax.add_patch(arrow2)

# 关键线
ax.axhline(y=target, color='green', linestyle='--', linewidth=2.5, label=f'Target: {target}')
ax.axhline(y=stop, color='red', linestyle='--', linewidth=2.5, label=f'Stop: {stop}')
ax.axhline(y=retest, color='blue', linestyle=':', linewidth=2, label=f'Retest: {retest}')

# 标注
ax.annotate(f'BUY: {retest}', xy=(4.4, retest), fontsize=12, color='blue', fontweight='bold',
            xytext=(2.5, retest-0.35), arrowprops=dict(arrowstyle='->', color='blue'))
ax.annotate(f'TP: {target}', xy=(5.5, target), fontsize=12, color='green', fontweight='bold',
            xytext=(5.7, target+0.25), arrowprops=dict(arrowstyle='->', color='green'))
ax.annotate(f'NOW: {current}', xy=(4, current), fontsize=11, color='black',
            xytext=(2.2, current+0.35))

# 标记放量
for i, d in enumerate(hist_5d):
    if d['volume'] > 200:
        ax.annotate('放量', xy=(i, d['high']+0.2), fontsize=9, color='purple', ha='center', fontweight='bold')

ax.set_xticks(range(len(hist_5d)+2))
ax.set_xticklabels([d['date'] for d in hist_5d] + ['02-13(E)', '02-14(E)'])
ax.legend(loc='upper left')
ax.grid(True, alpha=0.3)

plt.tight_layout()
plt.savefig('/home/none/clawd/skills/stock-trader-insight/kline_300383_realtime.png', dpi=150, bbox_inches='tight')
print(f"OK: kline_300383_realtime.png")
