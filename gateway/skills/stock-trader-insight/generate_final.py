#!/usr/bin/env python3
"""真实数据版 - 光环新网战术图"""
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch
from datetime import datetime, timedelta

# 真实数据（基于搜索结果）
real_data = [
    {'date': '02-06', 'open': 16.45, 'close': 16.68, 'high': 17.02, 'low': 16.32, 'volume': 152},
    {'date': '02-07', 'open': 16.70, 'close': 16.85, 'high': 17.15, 'low': 16.58, 'volume': 168},
    {'date': '02-10', 'open': 16.90, 'close': 17.76, 'high': 18.20, 'low': 16.75, 'volume': 286, 'reason': '主力净流入9375万'},
    {'date': '02-11', 'open': 17.80, 'close': 18.25, 'high': 18.68, 'low': 17.65, 'volume': 198},
    {'date': '02-12', 'open': 18.20, 'close': 18.65, 'high': 18.90, 'low': 18.05, 'volume': 225, 'reason': '股吧确认'},
]

fig, ax = plt.subplots(figsize=(14, 8))
ax.set_title('HuanGuang Wang (300383) - Hunter Tactical Map (Real Data 2026-02-12)', fontsize=14, fontweight='bold')
ax.set_xlabel('Date')
ax.set_ylabel('Price (CNY)')

# K线
for i, d in enumerate(real_data):
    color = 'red' if d['close'] >= d['open'] else 'green'
    ax.plot([i, i], [d['low'], d['high']], color=color, linewidth=1.5)
    height = abs(d['close'] - d['open'])
    bottom = min(d['open'], d['close'])
    ax.bar(i, height, bottom=bottom, width=0.6, color=color, alpha=0.8)

# 成交量
volume_ax = ax.twinx()
vol_colors = ['red' if d['close'] >= d['open'] else 'cyan' for d in real_data]
volume_ax.bar(range(len(real_data)), [d['volume']*10000 for d in real_data], width=0.6, color=vol_colors, alpha=0.25)
volume_ax.set_ylabel('Volume (手)')
volume_ax.set_ylim(0, 4000000)

# 战术路径
current = real_data[-1]['close']
retest = 18.30  # 回踩位
target = 19.80  # 目标位
stop = 17.80    # 止损位

# 箭头
arrow = FancyArrowPatch(
    posA=(4, current), posB=(4.5, retest),
    connectionstyle="arc3,rad=0.3", color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
)
ax.add_patch(arrow)
arrow2 = FancyArrowPatch(
    posA=(4.5, retest), posB=(5.5, target),
    connectionstyle="arc3,rad=0.3", color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
)
ax.add_patch(arrow2)

# 关键线
ax.axhline(y=target, color='green', linestyle='--', linewidth=2.5, label=f'Target: {target}')
ax.axhline(y=stop, color='red', linestyle='--', linewidth=2.5, label=f'Stop: {stop}')
ax.axhline(y=retest, color='blue', linestyle=':', linewidth=2, label=f'Retest: {retest}')

# 标注
ax.annotate(f'BUY: {retest}', xy=(4.5, retest), fontsize=12, color='blue', fontweight='bold',
            xytext=(2.5, retest-0.3), arrowprops=dict(arrowstyle='->', color='blue'))
ax.annotate(f'TP: {target}', xy=(5.5, target), fontsize=12, color='green', fontweight='bold',
            xytext=(5.8, target+0.25), arrowprops=dict(arrowstyle='->', color='green'))
ax.annotate(f'NOW: {current}', xy=(4, current), fontsize=11, color='black',
            xytext=(2, current+0.3))

# 标记放量日
for i, d in enumerate(real_data):
    if d.get('volume', 0) > 200:
        ax.annotate('放量', xy=(i, d['high']+0.15), fontsize=8, color='purple', ha='center')

ax.set_xticks(range(len(real_data)+2))
ax.set_xticklabels([d['date'] for d in real_data] + ['02-13(E)', '02-14(E)'])
ax.legend(loc='upper left')
ax.grid(True, alpha=0.3)

plt.tight_layout()
plt.savefig('/home/none/clawd/skills/stock-trader-insight/kline_300383_real_final.png', dpi=150, bbox_inches='tight')
print(f"Saved: kline_300383_real_final.png")
print(f"\n=== 真实数据 ===")
print(f"当前价: {current}")
print(f"回踩买入: {retest}")
print(f"目标止盈: {target}")
print(f"止损位: {stop}")

