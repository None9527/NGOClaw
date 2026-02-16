import numpy as np
import pandas as pd
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch
from datetime import datetime, timedelta

# --- 模拟数据 ---
np.random.seed(42) # 确保结果可复现

# 生成近10个交易日的日期
dates = [datetime.now() - timedelta(days=x) for x in range(10, 0, -1)]
# 模拟近似的真实K线数据 (OHLCV)
# 基于光环新网近期17.76 -> 18.52的走势
opens = [17.6 + i*0.05 + np.random.normal(0, 0.05) for i in range(10)]
closes = [o + np.random.normal(0.1, 0.15) for o in opens]
highs = [max(o, c) + abs(np.random.normal(0, 0.05)) for o, c in zip(opens, closes)]
lows = [min(o, c) - abs(np.random.normal(0, 0.05)) for o, c in zip(opens, closes)]
volumes = [np.random.uniform(20000000, 50000000) for _ in range(10)] # 模拟成交量

# 添加今天的收盘价 (18.52)
dates.append(datetime.now())
opens.append(closes[-1])
closes.append(18.52)
highs.append(max(opens[-1], closes[-1]) + abs(np.random.normal(0, 0.05)))
lows.append(min(opens[-1], closes[-1]) - abs(np.random.normal(0, 0.05)))
volumes.append(441900000) # 2月12日的实际成交额（单位调整）

df = pd.DataFrame({
    'Date': dates,
    'Open': opens,
    'High': highs,
    'Low': lows,
    'Close': closes,
    'Volume': volumes
})

# --- 图表设置 ---
fig, ax = plt.subplots(figsize=(12, 8))
ax.set_title('光环新网 (300383) - 猎手战术图 (Tactical Map)', fontsize=16)
ax.set_xlabel('日期')
ax.set_ylabel('价格')

# --- 绘制K线 ---
colors = ['red' if close >= open else 'green' for open, close in zip(df['Open'], df['Close'])]
for i in range(len(df)):
    # High-Low 线
    ax.plot([i, i], [df['Low'][i], df['High'][i]], color=colors[i])
    # Open-Close 矩形 (蜡烛实体)
    height = abs(df['Close'][i] - df['Open'][i])
    bottom = min(df['Open'][i], df['Close'][i])
    ax.bar(i, height, bottom=bottom, width=0.6, color=colors[i], alpha=0.8)

# --- 绘制彩色成交量 ---
volume_ax = ax.twinx()
volume_colors = ['red' if close >= open else 'cyan' for open, close in zip(df['Open'], df['Close'])]
volume_ax.bar(range(len(df)), df['Volume'], width=0.6, color=volume_colors, alpha=0.3)
volume_ax.set_ylabel('成交量')
volume_ax.set_ylim(0, max(df['Volume'])*1.2) # 设置合适的y轴范围

# --- 绘制预测路径 ---
# 假设明天 (index=10) 会先回踩到 18.3，然后拉升到 19.8
retest_price = 18.3
launch_target = 19.8
current_close = df['Close'].iloc[-1]

# 创建 FancyArrowPatch 对象
arrow = FancyArrowPatch(
    posA=(len(df)-1, current_close), # 起点：今天收盘价
    posB=(len(df)-1+0.5, retest_price), # 中点：明天回踩
    connectionstyle="arc3,rad=0.3", # 弧形连接
    color='orange',
    linewidth=2,
    arrowstyle='-|>',
    mutation_scale=20
)
ax.add_patch(arrow)

arrow2 = FancyArrowPatch(
    posA=(len(df)-1+0.5, retest_price), # 起点：回踩
    posB=(len(df), launch_target), # 终点：目标
    connectionstyle="arc3,rad=0.3", # 弧形连接
    color='orange',
    linewidth=2,
    arrowstyle='-|>',
    mutation_scale=20
)
ax.add_patch(arrow2)

# --- 标记目标价和止损价 ---
stop_loss = 18.0
ax.axhline(y=launch_target, color='green', linestyle='--', linewidth=2, label=f'目标价: {launch_target}')
ax.axhline(y=stop_loss, color='red', linestyle='--', linewidth=2, label=f'止损价: {stop_loss}')

# --- 标记买点区和盈利区 ---
ax.text(len(df)-1, current_close, '买点区?', ha='center', va='bottom', bbox=dict(boxstyle="round,pad=0.3", facecolor='yellow', alpha=0.5))
ax.text(len(df), launch_target, '盈利区', ha='center', va='bottom', bbox=dict(boxstyle="round,pad=0.3", facecolor='lightgreen', alpha=0.5))

# --- 设置x轴标签 ---
ax.set_xticks(range(len(df)+1)) # 为今天的预测也预留一个刻度
ax.set_xticklabels([d.strftime('%m-%d') for d in dates] + ['明(2/13)'])

# --- 图例 ---
ax.legend(loc='upper left')

# --- 保存图片 ---
plt.tight_layout()
plt.savefig('/home/none/clawd/skills/stock-trader-insight/kline_300383_pred_v3.png', dpi=150)
print("Tactical Map saved as kline_300383_pred_v3.png")

# plt.show() # 不显示，直接保存