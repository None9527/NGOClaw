#!/usr/bin/env python3
"""
获取真实股票数据 - 使用akshare从东方财富获取
"""
import akshare as ak
import pandas as pd
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch
from datetime import datetime, timedelta
import warnings
warnings.filterwarnings('ignore')

def get_stock_data(stock_code):
    """获取光环新网(300383)的日线数据"""
    try:
        # 获取日K线数据
        df = ak.stock_zh_a_hist(symbol=stock_code, period="daily", 
                                 start_date="20250101", end_date="20250212", adjust="qfq")
        df.columns = ['Date', 'Open', 'Close', 'High', 'Low', 'Volume', 'Turnover']
        df['Date'] = pd.to_datetime(df['Date'])
        return df
    except Exception as e:
        print(f"获取数据失败: {e}")
        return None

def generate_tactical_chart(df, stock_code="300383"):
    """生成真实数据的战术图"""
    if df is None or len(df) < 5:
        print("数据不足")
        return
    
    fig, ax = plt.subplots(figsize=(14, 8))
    ax.set_title(f'HuanGuang Wang ({stock_code}) - Hunter Tactical Map', fontsize=14)
    ax.set_xlabel('Date')
    ax.set_ylabel('Price (CNY)')
    
    # 绘制K线
    colors = ['red' if close >= open else 'green' for open, close in zip(df['Open'], df['Close'])]
    for i in range(len(df)):
        color = colors[i]
        # 上下影线
        ax.plot([i, i], [df['Low'][i], df['High'][i]], color=color, linewidth=1)
        # 实体
        height = abs(df['Close'][i] - df['Open'][i])
        bottom = min(df['Open'][i], df['Close'][i])
        ax.bar(i, height, bottom=bottom, width=0.6, color=color, alpha=0.8)
    
    # 成交量副图
    volume_ax = ax.twinx()
    volume_colors = ['red' if close >= open else 'cyan' for open, close in zip(df['Open'], df['Close'])]
    volume_ax.bar(range(len(df)), df['Volume'], width=0.6, color=volume_colors, alpha=0.3)
    volume_ax.set_ylabel('Volume')
    volume_ax.set_ylim(0, max(df['Volume']) * 1.5)
    
    # 战术路径 - 基于真实数据分析
    current_close = df['Close'].iloc[-1]
    recent_low = df['Low'].min()
    recent_high = df['High'].max()
    
    # 计算支撑和目标
    retest_price = round(current_close * 0.988, 2)  # 回踩位
    launch_target = round(current_close * 1.08, 2)   # 目标位 (+8%)
    stop_loss = round(current_close * 0.97, 2)       # 止损位
    
    # 绘制预测路径
    arrow = FancyArrowPatch(
        posA=(len(df)-1, current_close),
        posB=(len(df)-1 + 0.4, retest_price),
        connectionstyle="arc3,rad=0.3",
        color='orange',
        linewidth=3,
        arrowstyle='-|>',
        mutation_scale=20
    )
    ax.add_patch(arrow)
    
    arrow2 = FancyArrowPatch(
        posA=(len(df)-1 + 0.4, retest_price),
        posB=(len(df) + 0.3, launch_target),
        connectionstyle="arc3,rad=0.3",
        color='orange',
        linewidth=3,
        arrowstyle='-|>',
        mutation_scale=20
    )
    ax.add_patch(arrow2)
    
    # 目标线和止损线
    ax.axhline(y=launch_target, color='green', linestyle='--', linewidth=2.5, label=f'Target: {launch_target}')
    ax.axhline(y=stop_loss, color='red', linestyle='--', linewidth=2.5, label=f'Stop: {stop_loss}')
    ax.axhline(y=retest_price, color='blue', linestyle=':', linewidth=1.5, label=f'Retest: {retest_price}')
    
    # 标记区域
    ax.annotate('BUY ZONE', xy=(len(df)-1+0.4, retest_price), xytext=(len(df)-2, retest_price-0.3),
                fontsize=10, color='blue', fontweight='bold',
                arrowprops=dict(arrowstyle='->', color='blue'))
    
    ax.annotate('TAKE PROFIT', xy=(len(df)+0.3, launch_target), xytext=(len(df)+0.5, launch_target+0.2),
                fontsize=10, color='green', fontweight='bold',
                arrowprops=dict(arrowstyle='->', color='green'))
    
    # X轴标签
    ax.set_xticks(range(0, len(df), max(1, len(df)//10)))
    ax.set_xticklabels([d.strftime('%m-%d') for d in df['Date'].iloc[::max(1, len(df)//10)]], rotation=45)
    
    ax.legend(loc='upper left')
    ax.grid(True, alpha=0.3)
    
    plt.tight_layout()
    output_file = f'/home/none/clawd/skills/stock-trader-insight/kline_{stock_code}_real.png'
    plt.savefig(output_file, dpi=150, bbox_inches='tight')
    print(f"Tactical chart saved: {output_file}")
    
    return {
        'current': current_close,
        'retest': retest_price,
        'target': launch_target,
        'stop': stop_loss
    }

if __name__ == "__main__":
    print("Fetching real data for 300383...")
    df = get_stock_data("300383")
    if df is not None:
        print(f"Got {len(df)} days of data")
        print(df.tail())
        result = generate_tactical_chart(df, "300383")
        print(f"\nTactical Levels: {result}")
