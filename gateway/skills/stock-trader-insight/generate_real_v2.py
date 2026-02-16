#!/usr/bin/env python3
"""获取真实股票数据 - 修复版"""
import akshare as ak
import pandas as pd
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch
import warnings
warnings.filterwarnings('ignore')

def get_stock_data(stock_code):
    """获取日线数据"""
    try:
        df = ak.stock_zh_a_hist(symbol=stock_code, period="daily", 
                                 start_date="20250101", end_date="20250212", adjust="qfq")
        print(f"原始列: {df.columns.tolist()}")
        print(df.head(2))
        return df
    except Exception as e:
        print(f"获取数据失败: {e}")
        return None

def generate_tactical_chart():
    """生成战术图"""
    df = get_stock_data("300383")
    if df is None:
        return None
    
    # 使用原始列名
    df = df.rename(columns={
        df.columns[0]: 'Date',
        df.columns[1]: 'Open',
        df.columns[2]: 'Close', 
        df.columns[3]: 'High',
        df.columns[4]: 'Low',
        df.columns[5]: 'Volume',
        df.columns[6]: 'Turnover'
    })
    
    print(f"数据形状: {df.shape}")
    print(df.tail(3))
    
    fig, ax = plt.subplots(figsize=(14, 8))
    ax.set_title('HuanGuang Wang (300383) - Hunter Tactical Map', fontsize=14)
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
    
    # 战术路径
    current_close = df['Close'].iloc[-1]
    retest_price = round(current_close * 0.988, 2)
    launch_target = round(current_close * 1.08, 2)
    stop_loss = round(current_close * 0.97, 2)
    
    # 画箭头
    arrow = FancyArrowPatch(
        posA=(len(df)-1, current_close),
        posB=(len(df)-1 + 0.4, retest_price),
        connectionstyle="arc3,rad=0.3",
        color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
    )
    ax.add_patch(arrow)
    
    arrow2 = FancyArrowPatch(
        posA=(len(df)-1 + 0.4, retest_price),
        posB=(len(df) + 0.3, launch_target),
        connectionstyle="arc3,rad=0.3",
        color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
    )
    ax.add_patch(arrow2)
    
    # 目标线和止损线
    ax.axhline(y=launch_target, color='green', linestyle='--', linewidth=2.5, label=f'Target: {launch_target}')
    ax.axhline(y=stop_loss, color='red', linestyle='--', linewidth=2.5, label=f'Stop: {stop_loss}')
    ax.axhline(y=retest_price, color='blue', linestyle=':', linewidth=1.5, label=f'Retest: {retest_price}')
    
    # 标签
    ax.annotate(f'BUY: {retest_price}', xy=(len(df)-1+0.4, retest_price), fontsize=11, color='blue', fontweight='bold')
    ax.annotate(f'TP: {launch_target}', xy=(len(df)+0.3, launch_target), fontsize=11, color='green', fontweight='bold')
    
    ax.set_xticks(range(0, len(df), max(1, len(df)//8)))
    ax.set_xticklabels([str(d)[:10] for d in df['Date'].iloc[::max(1, len(df)//8)]], rotation=45)
    ax.legend(loc='upper left')
    ax.grid(True, alpha=0.3)
    
    plt.tight_layout()
    output = '/home/none/clawd/skills/stock-trader-insight/kline_300383_real.png'
    plt.savefig(output, dpi=150, bbox_inches='tight')
    print(f"\nSaved: {output}")
    print(f"Levels: Current={current_close}, Retest={retest_price}, Target={launch_target}, Stop={stop_loss}")

if __name__ == "__main__":
    generate_tactical_chart()
