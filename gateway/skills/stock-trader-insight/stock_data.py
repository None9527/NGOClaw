#!/usr/bin/env python3
"""
Stock data fetcher using Sina Finance API.
Supports realtime quotes and historical K-line data for A-shares.
Usage:
    python stock_data.py quote 600519 300383 000001
    python stock_data.py kline 600519 --days 30 --period daily
    python stock_data.py chart 300383 --days 20
"""
import sys
import json
import re
import urllib.request
from datetime import datetime, timedelta


SINA_REALTIME_URL = "http://hq.sinajs.cn/list={codes}"
SINA_KLINE_URL = (
    "https://money.finance.sina.com.cn/quotes_service/api/json_v2.php"
    "/CN_MarketData.getKLineData?symbol={symbol}&scale={scale}"
    "&ma=no&datalen={datalen}"
)

PERIOD_MAP = {
    "5min": 5, "15min": 15, "30min": 30, "60min": 60,
    "daily": 240, "weekly": 1680,
}


def _market_prefix(code: str) -> str:
    """Determine market prefix: sh for Shanghai, sz for Shenzhen."""
    code = code.strip()
    if code.startswith(("sh", "sz")):
        return code
    if code.startswith(("6", "5", "9", "11")):
        return f"sh{code}"
    return f"sz{code}"


def get_realtime(codes: list[str]) -> list[dict]:
    """Fetch realtime quotes from Sina Finance."""
    symbols = [_market_prefix(c) for c in codes]
    url = SINA_REALTIME_URL.format(codes=",".join(symbols))
    req = urllib.request.Request(url, headers={
        "Referer": "https://finance.sina.com.cn",
        "User-Agent": "Mozilla/5.0",
    })
    with urllib.request.urlopen(req, timeout=10) as resp:
        raw = resp.read().decode("gbk", errors="replace")

    results = []
    for line in raw.strip().split("\n"):
        line = line.strip().rstrip(";")
        if not line or '="' not in line:
            continue
        var_name, data_str = line.split('="', 1)
        symbol = var_name.split("_")[-1]
        parts = data_str.rstrip('"').split(",")
        if len(parts) < 32:
            continue
        results.append({
            "symbol": symbol,
            "name": parts[0],
            "open": float(parts[1]) if parts[1] else 0,
            "pre_close": float(parts[2]) if parts[2] else 0,
            "price": float(parts[3]) if parts[3] else 0,
            "high": float(parts[4]) if parts[4] else 0,
            "low": float(parts[5]) if parts[5] else 0,
            "volume": int(float(parts[8])) if parts[8] else 0,
            "turnover": float(parts[9]) if parts[9] else 0,
            "date": parts[30],
            "time": parts[31],
            "change": round(float(parts[3]) - float(parts[2]), 3) if parts[3] and parts[2] else 0,
            "change_pct": round((float(parts[3]) - float(parts[2])) / float(parts[2]) * 100, 2) if parts[3] and parts[2] and float(parts[2]) != 0 else 0,
        })
    return results


def get_kline(code: str, period: str = "daily", days: int = 30) -> list[dict]:
    """Fetch K-line data from Sina Finance.
    Args:
        code: Stock code like '600519' or 'sh600519'
        period: 5min, 15min, 30min, 60min, daily, weekly
        days: Number of data points to fetch
    Returns:
        List of dicts with keys: date, open, high, low, close, volume
    """
    symbol = _market_prefix(code)
    scale = PERIOD_MAP.get(period, 240)
    url = SINA_KLINE_URL.format(symbol=symbol, scale=scale, datalen=days)
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=10) as resp:
        raw = resp.read().decode("utf-8")

    data = json.loads(raw)
    results = []
    for item in data:
        results.append({
            "date": item["day"],
            "open": float(item["open"]),
            "high": float(item["high"]),
            "low": float(item["low"]),
            "close": float(item["close"]),
            "volume": int(item["volume"]),
        })
    return results


def generate_chart(code: str, days: int = 20, output: str = None):
    """Generate a tactical K-line chart with matplotlib."""
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    from matplotlib.patches import FancyArrowPatch
    plt.rcParams["font.sans-serif"] = ["Noto Sans CJK SC", "WenQuanYi Micro Hei", "SimHei", "DejaVu Sans"]
    plt.rcParams["axes.unicode_minus"] = False

    kline = get_kline(code, "daily", days)
    if not kline:
        print(json.dumps({"error": "No kline data"}))
        return None

    quote = get_realtime([code])
    name = quote[0]["name"] if quote else code

    dates = [k["date"] for k in kline]
    opens = [k["open"] for k in kline]
    highs = [k["high"] for k in kline]
    lows = [k["low"] for k in kline]
    closes = [k["close"] for k in kline]
    volumes = [k["volume"] for k in kline]

    fig, ax = plt.subplots(figsize=(14, 8))
    ax.set_title(f"{name} ({code}) - Hunter Tactical Map", fontsize=14, fontweight="bold")
    ax.set_ylabel("Price (CNY)")

    colors = ["red" if c >= o else "green" for o, c in zip(opens, closes)]
    for i in range(len(kline)):
        ax.plot([i, i], [lows[i], highs[i]], color=colors[i], linewidth=1)
        h = abs(closes[i] - opens[i]) or 0.01
        b = min(opens[i], closes[i])
        ax.bar(i, h, bottom=b, width=0.6, color=colors[i], alpha=0.8)

    vol_ax = ax.twinx()
    vol_colors = ["red" if c >= o else "cyan" for o, c in zip(opens, closes)]
    vol_ax.bar(range(len(kline)), volumes, width=0.6, color=vol_colors, alpha=0.25)
    vol_ax.set_ylabel("Volume")

    current = closes[-1]
    retest = round(current * 0.988, 2)
    target = round(current * 1.075, 2)
    stop = round(current * 0.965, 2)

    n = len(kline)
    arrow1 = FancyArrowPatch(
        posA=(n - 1, current), posB=(n - 0.5, retest),
        connectionstyle="arc3,rad=0.3",
        color="orange", linewidth=3, arrowstyle="-|>", mutation_scale=20,
    )
    arrow2 = FancyArrowPatch(
        posA=(n - 0.5, retest), posB=(n + 0.3, target),
        connectionstyle="arc3,rad=0.3",
        color="orange", linewidth=3, arrowstyle="-|>", mutation_scale=20,
    )
    ax.add_patch(arrow1)
    ax.add_patch(arrow2)

    ax.axhline(y=target, color="green", linestyle="--", linewidth=2, label=f"Target: {target}")
    ax.axhline(y=stop, color="red", linestyle="--", linewidth=2, label=f"Stop: {stop}")
    ax.axhline(y=retest, color="blue", linestyle=":", linewidth=1.5, label=f"Retest: {retest}")

    ax.annotate(f"BUY: {retest}", xy=(n - 0.5, retest), fontsize=11, color="blue", fontweight="bold")
    ax.annotate(f"TP: {target}", xy=(n + 0.3, target), fontsize=11, color="green", fontweight="bold")
    ax.annotate(f"NOW: {current}", xy=(n - 1, current), fontsize=10, color="black")

    step = max(1, len(kline) // 8)
    ax.set_xticks(range(0, n, step))
    ax.set_xticklabels([dates[i][-5:] for i in range(0, n, step)], rotation=45)
    ax.legend(loc="upper left")
    ax.grid(True, alpha=0.3)
    plt.tight_layout()

    if not output:
        output = f"/home/none/clawd/skills/stock-trader-insight/kline_{code}_tactical.png"
    plt.savefig(output, dpi=150, bbox_inches="tight")
    plt.close()

    result = {
        "chart": output,
        "name": name,
        "code": code,
        "current": current,
        "retest": retest,
        "target": target,
        "stop": stop,
        "data_source": "Sina Finance API",
        "data_points": len(kline),
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return result


def main():
    if len(sys.argv) < 2:
        print("Usage:")
        print("  stock_data.py quote 600519 300383")
        print("  stock_data.py kline 600519 [--days 30] [--period daily]")
        print("  stock_data.py chart 300383 [--days 20]")
        sys.exit(1)

    cmd = sys.argv[1]
    args = sys.argv[2:]

    if cmd == "quote":
        codes = [a for a in args if not a.startswith("-")]
        data = get_realtime(codes)
        print(json.dumps(data, ensure_ascii=False, indent=2))

    elif cmd == "kline":
        code = args[0] if args else "600519"
        days = 30
        period = "daily"
        for i, a in enumerate(args):
            if a == "--days" and i + 1 < len(args):
                days = int(args[i + 1])
            elif a == "--period" and i + 1 < len(args):
                period = args[i + 1]
        data = get_kline(code, period, days)
        print(json.dumps(data, ensure_ascii=False, indent=2))

    elif cmd == "chart":
        code = args[0] if args else "600519"
        days = 20
        for i, a in enumerate(args):
            if a == "--days" and i + 1 < len(args):
                days = int(args[i + 1])
        generate_chart(code, days)

    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)


if __name__ == "__main__":
    main()
