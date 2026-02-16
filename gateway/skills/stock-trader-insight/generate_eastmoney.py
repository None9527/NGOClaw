#!/usr/bin/env python3
"""使用requests直接获取东方财富K线数据"""
import requests
import json
import pandas as pd
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch
import warnings
warnings.filterwarnings('ignore')

def get_kline_from_eastmoney():
    """从东方财富API获取K线数据"""
    url = "https://push2.eastmoney.com/api/qt/stock/kline/get"
    params = {
        "secid": "1.300383",  # 深股
        "fields1": "f1,f2,f3,f4,f5,f6",
        "fields2": "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61",
        "klt": "101",  # 日K
        "fqt": "1",    # 前复权
        "beg": "0",
        "end": "20500101",
        "lmt": "20"    # 最近20个交易日
    }
    
    try:
        r = requests.get(url, params=params, timeout=10)
        data = r.json()
        if data["data"]["klines"]:
            klines = data["data"]["klines"]
            records = []
            for k in klines:
                parts = k.split(",")
                records.append({
                    'Date': parts[0],
                    'Open': float(parts[1]),
                    'Close': float(parts[2]),
                    'High': float(parts[3]),
                    'Low': float(parts[4]),
                    'Volume': float(parts[5]),
                    'Turnover': float(parts[6])
                })
            return pd.DataFrame(records)
    except Exception as e:
        print(f"API Error: {e}")
    return None

def generate_tactical_chart():
    """生成真实数据战术图"""
    df = get_kline_from_eastmoney()
    if df is None:
        print("无法获取数据")
        return None
    
    print(f"获取到 {len(df)} 天数据")
    print(df.tail(3))
    
    current_close = df['Close'].iloc[-1]
    print(f"\n最新收盘价: {current_close}")
    
    fig, ax = plt.subplots(figsize=(14, 8))
    ax.set_title('HuanGuang Wang (300383) - Hunter Tactical Map (Real Data)', fontsize=14)
    ax.set_xlabel('Date')
    ax.set_ylabel('Price (CNY)')
    
    # 绘制K线
    colors = ['red' if close >= open else 'green' for open, close in zip(df['Open'], df['Close'])]
    for i in range(len(df)):
        color = colors[i]
        ax.plot([i, i], [df['Low'].iloc[i], df['High'].iloc[i]], color=color, linewidth=1)
        height = abs(df['Close'].iloc[i] - df['Open'].iloc[i])
        bottom = min(df['Open'].iloc[i], df['Close'].iloc[i])
        ax.bar(i, height, bottom=bottom, width=0.6, color=color, alpha=0.8)
    
    # 成交量
    volume_ax = ax.twinx()
    volume_colors = ['red' if close >= open else 'cyan' for open, close in zip(df['Open'], df['Close'])]
    volume_ax.bar(range(len(df)), df['Volume'], width=0.6, color=volume_colors, alpha=0.3)
    volume_ax.set_ylabel('Volume')
    
    # 战术路径 - 基于真实数据分析
    retest_price = round(current_close * 0.988, 2)  # 回踩位
    launch_target = round(current_close * 1.075, 2)  # 目标位 (+7.5%)
    stop_loss = round(current_close * 0.965, 2)      # 止损位
    
    # 画箭头路径
    arrow = FancyArrowPatch(
        posA=(len(df)-1, current_close),
        posB=(len(df)-1 + 0.5, retest_price),
        connectionstyle="arc3,rad=0.3",
        color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
    )
    ax.add_patch(arrow)
    
    arrow2 = FancyArrowPatch(
        posA=(len(df)-1 + 0.5, retest_price),
        posB=(len(df) + 0.4, launch_target),
        connectionstyle="arc3,rad=0.3",
        color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
    )
    ax.add_patch(arrow2)
    
    # 目标线和止损线
    ax.axhline(y=launch_target, color='green', linestyle='--', linewidth=2.5, label=f'Target: {launch_target}')
    ax.axhline(y=stop_loss, color='red', linestyle='--', linewidth=2.5, label=f'Stop: {stop_loss}')
    ax.axhline(y=retest_price, color='blue', linestyle=':', linewidth=1.5, label=f'Retest: {retest_price}')
    
    # 标注
    ax.annotate(f'BUY: {retest_price}', xy=(len(df)-1+0.5, retest_price), 
                fontsize=12, color='blue', fontweight='bold',
                xytext=(len(df)-3, retest_price-0.2),
                arrowprops=dict(arrowstyle='->', color='blue'))
    
    ax.annotate(f'TP: {launch_target}', xy=(len(df)+0.4, launch_target), 
                fontsize=12, color='green', fontweight='bold',
                xytext=(len(df)+0.5, launch_target+0.15),
                arrowprops=dict(arrowstyle='->', color='green'))
    
    # 当前价标注
    ax.annotate(f'Now: {current_close}', xy=(len(df)-1, current_close),
                fontsize=10, color='black', xytext=(len(df)-3, current_close+0.2))
    
    ax.set_xticks(range(0, len(df), max(1, len(df)//10)))
    ax.set_xticklabels(df['Date'].iloc[::max(1, len(df)//10)], rotation=45)
    ax.legend(loc='upper left')
    ax.grid(True, alpha=0.3)
    
    plt.tight_layout()
    output = '/home/none/clawd/skills/stock-trader-insight/kline_300383_real.png'
    plt.savefig(output, dpi=150, bbox_inches='tight')
    print(f"\nChart saved: {output}")
    
    return {'current': current_close, 'retest': retest_price, 'target': launch_target, 'stop': stop_loss}

if __name__ == "__main__":
    result = generate_tactical_chart()
    if result:
        print(f"\n=== 猎杀指令 ===")
        print(f"当前价: {result['current']}")
        print(f"回踩买入: {result['retest']}")
        print(f"目标止盈: {result['target']}")
        print(f"止损位: {result['stop']}")

