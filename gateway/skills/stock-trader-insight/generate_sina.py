#!/usr/bin/env python3
"""新浪API实时版 - 光环新网战术图"""
import matplotlib.pyplot as plt
from matplotlib.patches import FancyArrowPatch
import subprocess
import json

def get_sina_kline(symbol, days=5):
    """获取新浪K线数据"""
    hist = []
    for i in range(days):
        cmd = f"curl -s --connect-timeout 5 -H 'Referer: http://finance.sina.com.cn' 'http://hq.sinajs.cn/list={symbol}' 2>&1 | iconv -f GBK -t UTF-8"
        try:
            result = subprocess.check_output(cmd, shell=True).decode('utf-8')
            if 'var hq_str' in result:
                parts = result.split('"')[1].split(',')
                if len(parts) > 30:
                    hist.append({
                        'date': parts[30],
                        'open': float(parts[1]),
                        'close': float(parts[3]),
                        'high': float(parts[4]),
                        'low': float(parts[5]),
                        'volume': int(parts[7]) / 10000,  # 手
                    })
        except:
            pass
    return hist

def draw_tactical_chart():
    """绘制战术图"""
    data = get_sina_kline('sz300383', 5)
    if not data:
        print("Failed to get data")
        return
    
    current = data[-1]['close']
    retest = round(current * 0.988, 2)
    target = round(current * 1.075, 2)
    stop = round(current * 0.965, 2)
    
    fig, ax = plt.subplots(figsize=(14, 8))
    ax.set_title('HuanGuang Wang (300383) - Hunter Tactical Map (Sina Real-Time)', fontsize=14, fontweight='bold')
    ax.set_xlabel('Date')
    ax.set_ylabel('Price (CNY)')
    
    # K线
    for i, d in enumerate(data):
        color = 'red' if d['close'] >= d['open'] else 'green'
        ax.plot([i, i], [d['low'], d['high']], color=color, linewidth=1.5)
        height = abs(d['close'] - d['open'])
        bottom = min(d['open'], d['close'])
        ax.bar(i, height, bottom=bottom, width=0.6, color=color, alpha=0.8)
    
    # 成交量
    volume_ax = ax.twinx()
    vol_colors = ['red' if d['close'] >= d['open'] else 'cyan' for d in data]
    volume_ax.bar(range(len(data)), [d['volume']*10000 for d in data], width=0.6, color=vol_colors, alpha=0.25)
    volume_ax.set_ylabel('Volume')
    
    # 战术路径
    arrow = FancyArrowPatch(
        posA=(len(data)-1, current), posB=(len(data)-0.5, retest),
        connectionstyle="arc3,rad=0.3", color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
    )
    ax.add_patch(arrow)
    arrow2 = FancyArrowPatch(
        posA=(len(data)-0.5, retest), posB=(len(data)+0.5, target),
        connectionstyle="arc3,rad=0.3", color='orange', linewidth=3, arrowstyle='-|>', mutation_scale=20
    )
    ax.add_patch(arrow2)
    
    # 关键线
    ax.axhline(y=target, color='green', linestyle='--', linewidth=2.5, label=f'Target: {target}')
    ax.axhline(y=stop, color='red', linestyle='--', linewidth=2.5, label=f'Stop: {stop}')
    ax.axhline(y=retest, color='blue', linestyle=':', linewidth=2, label=f'Retest: {retest}')
    
    # 标注
    ax.annotate(f'BUY: {retest}', xy=(len(data)-0.5, retest), fontsize=12, color='blue', fontweight='bold',
                xytext=(len(data)-2.5, retest-0.3), arrowprops=dict(arrowstyle='->', color='blue'))
    ax.annotate(f'TP: {target}', xy=(len(data)+0.5, target), fontsize=12, color='green', fontweight='bold',
                xytext=(len(data)+0.6, target+0.25), arrowprops=dict(arrowstyle='->', color='green'))
    ax.annotate(f'NOW: {current}', xy=(len(data)-1, current), fontsize=11, color='black',
                xytext=(len(data)-3, current+0.25))
    
    ax.set_xticks(range(len(data)+1))
    ax.set_xticklabels([d['date'][5:] for d in data] + ['Tomorrow'])
    ax.legend(loc='upper left')
    ax.grid(True, alpha=0.3)
    
    plt.tight_layout()
    output = '/home/none/clawd/skills/stock-trader-insight/kline_300383_sina.png'
    plt.savefig(output, dpi=150, bbox_inches='tight')
    print(f"OK: {output}")
    
    print(f"\n=== REAL-TIME DATA ===")
    for d in data:
        print(f"{d['date']}: O={d['open']} C={d['close']} H={d['high']} L={d['low']} Vol={d['volume']:.1f}万")
    print(f"\nLevels: Now={current}, Buy={retest}, TP={target}, Stop={stop}")

if __name__ == "__main__":
    draw_tactical_chart()
