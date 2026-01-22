package tools

const ScriptFetchDailyData = `
import json
import datetime
import sys
from yahooquery import Ticker
import yfinance as yf
from GoogleNews import GoogleNews

ticker_symbol = "{{.Ticker}}"
print(f"--- Fetching Data for {ticker_symbol} ---")

# 1. 简介 (YahooQuery)
yq = Ticker(ticker_symbol)
summary = "N/A"
sector = "Unknown"
industry = "Unknown"
try:
    profile = yq.asset_profile.get(ticker_symbol, {})
    if isinstance(profile, str): 
        # 处理 ETF 或错误情况
        summary_data = yq.summary_profile.get(ticker_symbol, {})
        summary = summary_data.get('longDescription', 'No summary.')
        sector = summary_data.get('categoryName', 'Unknown')
    else:
        summary = profile.get('longBusinessSummary', 'No summary.')
        sector = profile.get('sector', 'Unknown')
        industry = profile.get('industry', 'Unknown')
except Exception as e:
    print(f"[WARN] Profile error: {e}")

# 2. 行情 (YFinance)
price_change = 0.0
last_price = 0.0
market_txt = "Market data unavailable"
try:
    hist = yf.Ticker(ticker_symbol).history(period="5d")
    if not hist.empty:
        last_price = hist['Close'].iloc[-1]
        prev = hist['Close'].iloc[-2] if len(hist)>1 else last_price
        price_change = ((last_price - prev) / prev) * 100
        market_txt = f"Price: ${last_price:.2f}, Change: {price_change:.2f}%"
except Exception as e:
    print(f"[WARN] Market error: {e}")

# 3. 新闻 (GoogleNews) - 可选，视 Docker 镜像是否安装而定
news_txt = "No news fetcher available."
try:
    from GoogleNews import GoogleNews
    gn = GoogleNews(period='3d')
    gn.search(f"{ticker_symbol} stock")
    news_items = []
    for item in gn.result()[:3]:
        news_items.append(f"- [{item.get('date')}] {item.get('title')}")
    if news_items:
        news_txt = "\n".join(news_items)
except Exception:
    pass # 忽略新闻模块缺失

# 4. 组合 RAG 专用文本
today = datetime.date.today().isoformat()
full_text = f"""
REPORT DATE: {today}
TICKER: {ticker_symbol}

=== [GRAPH_SAFE_START] ===
--- [BUSINESS SUMMARY] ---
{summary}

--- [RECENT NEWS] ---
{news_txt}
=== [GRAPH_SAFE_END] ===

--- [MARKET SNAPSHOT] ---
{market_txt}
"""

# 5. 保存文件
filename = f"{ticker_symbol}_{today}_raw.txt"
with open(filename, "w", encoding="utf-8") as f:
    f.write(full_text)

# 6. 输出协议
print(f"__FILE__:{filename}")

# 输出 JSON 元数据 (单行，方便 Go 解析)
meta = {
    "ticker": ticker_symbol,
    "date": today,
    "price_change": round(price_change, 2),
    "has_news": "GoogleNews" in sys.modules and len(news_txt) > 25
}
print(f"__META__:{json.dumps(meta)}")
print("Done.")
`
