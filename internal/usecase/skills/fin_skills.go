package skills

func init() {
	Register(skillCandlestickChart)
	Register(skillLineChart)
	Register(skillMultiStockCompare)
	Register(skillCompanyFinancials)
	Register(skillNewsSentiment)
	Register(skillTechnicalMACD)
	Register(skillTechnicalRSI)
	Register(skillTechnicalBollinger)
	Register(skillDataSummary)
}

// ─── Skill 1: K线图 (Candlestick) ───────────────────────────────────

var skillCandlestickChart = Skill{
	Name:        "candlestick_chart",
	Description: "使用 mplfinance 绘制专业 K 线图（含成交量、均线）",
	Keywords:    []string{"k线", "k线图", "candlestick", "蜡烛图", "ohlc", "mplfinance", "股价图"},
	APITips: `1. yfinance.download() 返回 MultiIndex 列，必须 df.columns = df.columns.get_level_values(0) 展平
2. mplfinance 要求 Index 为 DatetimeIndex 且 name='Date'
3. 列名必须是 Open/High/Low/Close/Volume（首字母大写）
4. 用 savefig 参数而不是 plt.savefig`,
	Template: `import yfinance as yf
import mplfinance as mpf
import pandas as pd

ticker = "AAPL"  # 替换为目标股票
period = "3mo"   # 1d/5d/1mo/3mo/6mo/1y/2y/5y/max

df = yf.download(ticker, period=period)

# CRITICAL: 展平多级列索引
if isinstance(df.columns, pd.MultiIndex):
    df.columns = df.columns.get_level_values(0)

# CRITICAL: mplfinance 要求 DatetimeIndex
df.index = pd.to_datetime(df.index)
df.index.name = 'Date'

if df.empty:
    print(f"Error: No data returned for {ticker}")
else:
    filename = f"{ticker.lower()}_candlestick.png"
    mpf.plot(
        df,
        type='candle',
        style='charles',
        title=f'{ticker} Candlestick Chart',
        ylabel='Price (USD)',
        volume=True,
        mav=(5, 20),
        figsize=(14, 8),
        savefig=filename,
    )
    print(f"__FILE__:{filename}")
    print(f"Successfully generated candlestick chart for {ticker}")
    print(f"Date range: {df.index[0].strftime('%Y-%m-%d')} to {df.index[-1].strftime('%Y-%m-%d')}")
    print(f"Latest close: ${df['Close'].iloc[-1]:.2f}")
`,
}

// ─── Skill 2: 收盘价折线图 ──────────────────────────────────────────

var skillLineChart = Skill{
	Name:        "line_chart",
	Description: "使用 matplotlib 绘制股价折线图（含均线叠加）",
	Keywords:    []string{"折线图", "line chart", "收盘价", "走势图", "价格趋势", "均线", "ma"},
	APITips: `1. yfinance.download() 返回 MultiIndex 列，必须展平
2. 用 plt.savefig() 保存，tight_layout 避免截断`,
	Template: `import yfinance as yf
import matplotlib.pyplot as plt
import pandas as pd

ticker = "AAPL"
period = "6mo"

df = yf.download(ticker, period=period)
if isinstance(df.columns, pd.MultiIndex):
    df.columns = df.columns.get_level_values(0)

if df.empty:
    print(f"Error: No data returned for {ticker}")
else:
    plt.figure(figsize=(14, 7))
    plt.plot(df.index, df['Close'], label='Close', linewidth=1.5)
    plt.plot(df.index, df['Close'].rolling(20).mean(), label='MA20', linestyle='--', alpha=0.8)
    plt.plot(df.index, df['Close'].rolling(60).mean(), label='MA60', linestyle='--', alpha=0.8)
    plt.fill_between(df.index, df['Low'], df['High'], alpha=0.1, color='blue', label='High-Low Range')
    plt.title(f'{ticker} Price Trend', fontsize=14)
    plt.xlabel('Date')
    plt.ylabel('Price (USD)')
    plt.legend()
    plt.grid(True, alpha=0.3)
    plt.tight_layout()
    filename = f"{ticker.lower()}_line.png"
    plt.savefig(filename, dpi=150)
    print(f"__FILE__:{filename}")
    print(f"Latest close: ${df['Close'].iloc[-1]:.2f}")
`,
}

// ─── Skill 3: 多股对比 ─────────────────────────────────────────────

var skillMultiStockCompare = Skill{
	Name:        "multi_stock_compare",
	Description: "多只股票收益率对比图（归一化）",
	Keywords:    []string{"对比", "compare", "比较", "多只", "vs", "多股", "收益率对比"},
	APITips: `1. 多股票下载时列是 MultiIndex: (Price, Ticker)
2. 用 df['Close'] 获取所有 ticker 的收盘价 DataFrame
3. 归一化: (price / price.iloc[0] - 1) * 100 转为百分比收益`,
	Template: `import yfinance as yf
import matplotlib.pyplot as plt
import pandas as pd

tickers = ["AAPL", "MSFT", "GOOGL"]  # 替换为目标股票列表
period = "6mo"

df = yf.download(tickers, period=period)

# 多股下载: df['Close'] 返回 DataFrame，每列是一只股票
closes = df['Close']
if closes.empty:
    print("Error: No data returned")
else:
    # 归一化为百分比收益
    returns = (closes / closes.iloc[0] - 1) * 100

    plt.figure(figsize=(14, 7))
    for col in returns.columns:
        plt.plot(returns.index, returns[col], label=col, linewidth=1.5)
    plt.title('Stock Performance Comparison', fontsize=14)
    plt.xlabel('Date')
    plt.ylabel('Return (%)')
    plt.legend(fontsize=12)
    plt.grid(True, alpha=0.3)
    plt.axhline(y=0, color='gray', linestyle='-', alpha=0.5)
    plt.tight_layout()
    filename = "stock_comparison.png"
    plt.savefig(filename, dpi=150)
    print(f"__FILE__:{filename}")

    # 打印统计
    for col in closes.columns:
        ret = (closes[col].iloc[-1] / closes[col].iloc[0] - 1) * 100
        print(f"{col}: {ret:+.2f}% (${closes[col].iloc[-1]:.2f})")
`,
}

// ─── Skill 4: 公司财务数据 ─────────────────────────────────────────

var skillCompanyFinancials = Skill{
	Name:        "company_financials",
	Description: "使用 yahooquery 获取公司财务摘要、估值指标、分析师评级",
	Keywords:    []string{"财务", "financial", "估值", "市盈率", "pe", "eps", "营收", "revenue", "利润", "profit", "分析师", "analyst", "yahooquery", "基本面"},
	APITips: `1. yahooquery 的 Ticker 用法: t = Ticker("AAPL")
2. t.summary_detail 返回 dict，键是 ticker
3. t.financial_data 返回 dict，键是 ticker
4. t.recommendation_trend 返回 DataFrame
5. 如果返回字符串而非 dict，表示查询失败`,
	Template: `from yahooquery import Ticker
import json

ticker_symbol = "AAPL"
t = Ticker(ticker_symbol)

# 基本信息
profile = t.summary_profile.get(ticker_symbol, {})
detail = t.summary_detail.get(ticker_symbol, {})
fin = t.financial_data.get(ticker_symbol, {})

if isinstance(profile, str):
    print(f"Error: Failed to fetch data for {ticker_symbol}: {profile}")
else:
    print(f"=== {ticker_symbol} Financial Summary ===")
    print(f"Company: {profile.get('longBusinessSummary', 'N/A')[:200]}...")
    print(f"\n--- Valuation ---")
    print(f"Market Cap: ${detail.get('marketCap', 0):,.0f}")
    print(f"P/E (Trailing): {detail.get('trailingPE', 'N/A')}")
    print(f"P/E (Forward): {detail.get('forwardPE', 'N/A')}")
    print(f"Dividend Yield: {detail.get('dividendYield', 0)*100:.2f}%")
    print(f"\n--- Financials ---")
    print(f"Revenue: ${fin.get('totalRevenue', 0):,.0f}")
    print(f"Profit Margin: {fin.get('profitMargins', 0)*100:.1f}%")
    print(f"Operating Margin: {fin.get('operatingMargins', 0)*100:.1f}%")
    print(f"ROE: {fin.get('returnOnEquity', 0)*100:.1f}%")
    print(f"Current Price: ${fin.get('currentPrice', 'N/A')}")
    print(f"Target Mean Price: ${fin.get('targetMeanPrice', 'N/A')}")
    print(f"Recommendation: {fin.get('recommendationKey', 'N/A')}")
`,
}

// ─── Skill 5: 新闻舆情分析 ─────────────────────────────────────────

var skillNewsSentiment = Skill{
	Name:        "news_sentiment",
	Description: "使用 GoogleNews 抓取新闻并用 TextBlob 做情感分析",
	Keywords:    []string{"新闻", "news", "舆情", "sentiment", "情感分析", "舆论", "googlenews"},
	APITips: `1. GoogleNews 库: from GoogleNews import GoogleNews
2. gn = GoogleNews(lang='en', period='7d')
3. gn.search('keyword') 然后 gn.results() 获取列表
4. 每条结果: {'title': ..., 'date': ..., 'desc': ..., 'link': ...}
5. TextBlob(text).sentiment.polarity 返回 -1.0 到 1.0`,
	Template: `from GoogleNews import GoogleNews
from textblob import TextBlob
import os

# GoogleNews 可能需要安装
try:
    gn = GoogleNews(lang='en', period='7d')
except:
    os.system('pip install GoogleNews textblob')
    from GoogleNews import GoogleNews
    from textblob import TextBlob
    gn = GoogleNews(lang='en', period='7d')

keyword = "AAPL Apple"  # 替换为目标关键词
gn.clear()
gn.search(keyword)
articles = gn.results()

if not articles:
    print(f"No news found for '{keyword}'")
else:
    print(f"=== {keyword} News Sentiment ({len(articles)} articles) ===\n")
    sentiments = []
    for i, art in enumerate(articles[:10]):  # 最多分析10条
        title = art.get('title', '')
        desc = art.get('desc', '')
        date = art.get('date', '')
        text = f"{title}. {desc}"
        polarity = TextBlob(text).sentiment.polarity
        sentiments.append(polarity)
        emoji = "🟢" if polarity > 0.1 else ("🔴" if polarity < -0.1 else "⚪")
        print(f"{emoji} [{date}] {title}")
        print(f"   Sentiment: {polarity:.3f}")
        print()

    avg = sum(sentiments) / len(sentiments) if sentiments else 0
    positive = sum(1 for s in sentiments if s > 0.1)
    negative = sum(1 for s in sentiments if s < -0.1)
    neutral = len(sentiments) - positive - negative
    print(f"--- Summary ---")
    print(f"Average sentiment: {avg:.3f}")
    print(f"Positive: {positive} | Neutral: {neutral} | Negative: {negative}")
    if avg > 0.1:
        print("Overall: BULLISH 📈")
    elif avg < -0.1:
        print("Overall: BEARISH 📉")
    else:
        print("Overall: NEUTRAL ➡️")
`,
}

// ─── Skill 6: MACD 技术指标 ────────────────────────────────────────

var skillTechnicalMACD = Skill{
	Name:        "technical_macd",
	Description: "计算并绘制 MACD 指标图（含信号线、柱状图）",
	Keywords:    []string{"macd", "技术指标", "技术分析", "信号线", "金叉", "死叉"},
	APITips: `1. MACD = EMA12 - EMA26, Signal = EMA9(MACD), Histogram = MACD - Signal
2. 不要用 ta-lib（未安装），用 pandas 的 ewm() 计算
3. 使用 subplot 画价格图和 MACD 图`,
	Template: `import yfinance as yf
import matplotlib.pyplot as plt
import pandas as pd

ticker = "AAPL"
period = "6mo"

df = yf.download(ticker, period=period)
if isinstance(df.columns, pd.MultiIndex):
    df.columns = df.columns.get_level_values(0)

if df.empty:
    print(f"Error: No data for {ticker}")
else:
    close = df['Close']
    ema12 = close.ewm(span=12, adjust=False).mean()
    ema26 = close.ewm(span=26, adjust=False).mean()
    macd = ema12 - ema26
    signal = macd.ewm(span=9, adjust=False).mean()
    histogram = macd - signal

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(14, 10), height_ratios=[2, 1], sharex=True)

    ax1.plot(df.index, close, label='Close', linewidth=1.5)
    ax1.set_title(f'{ticker} Price + MACD', fontsize=14)
    ax1.set_ylabel('Price (USD)')
    ax1.legend()
    ax1.grid(True, alpha=0.3)

    ax2.plot(df.index, macd, label='MACD', color='blue', linewidth=1)
    ax2.plot(df.index, signal, label='Signal', color='orange', linewidth=1)
    colors = ['green' if h >= 0 else 'red' for h in histogram]
    ax2.bar(df.index, histogram, color=colors, alpha=0.5, label='Histogram')
    ax2.axhline(y=0, color='gray', linestyle='-', alpha=0.5)
    ax2.set_ylabel('MACD')
    ax2.legend()
    ax2.grid(True, alpha=0.3)

    plt.tight_layout()
    filename = f"{ticker.lower()}_macd.png"
    plt.savefig(filename, dpi=150)
    print(f"__FILE__:{filename}")
    print(f"Latest MACD: {macd.iloc[-1]:.4f}, Signal: {signal.iloc[-1]:.4f}")
    if macd.iloc[-1] > signal.iloc[-1] and macd.iloc[-2] <= signal.iloc[-2]:
        print("Signal: GOLDEN CROSS (bullish) 📈")
    elif macd.iloc[-1] < signal.iloc[-1] and macd.iloc[-2] >= signal.iloc[-2]:
        print("Signal: DEATH CROSS (bearish) 📉")
`,
}

// ─── Skill 7: RSI 指标 ─────────────────────────────────────────────

var skillTechnicalRSI = Skill{
	Name:        "technical_rsi",
	Description: "计算并绘制 RSI 相对强弱指标（含超买超卖区间）",
	Keywords:    []string{"rsi", "相对强弱", "超买", "超卖", "overbought", "oversold"},
	APITips: `1. RSI = 100 - (100 / (1 + RS))，RS = avg_gain / avg_loss
2. 用 pandas diff() 和 rolling() 计算，不需要 ta-lib
3. 70 以上超买，30 以下超卖`,
	Template: `import yfinance as yf
import matplotlib.pyplot as plt
import pandas as pd

ticker = "AAPL"
period = "6mo"
rsi_period = 14

df = yf.download(ticker, period=period)
if isinstance(df.columns, pd.MultiIndex):
    df.columns = df.columns.get_level_values(0)

if df.empty:
    print(f"Error: No data for {ticker}")
else:
    close = df['Close']
    delta = close.diff()
    gain = delta.where(delta > 0, 0.0)
    loss = (-delta).where(delta < 0, 0.0)
    avg_gain = gain.rolling(window=rsi_period).mean()
    avg_loss = loss.rolling(window=rsi_period).mean()
    rs = avg_gain / avg_loss
    rsi = 100 - (100 / (1 + rs))

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(14, 9), height_ratios=[2, 1], sharex=True)

    ax1.plot(df.index, close, linewidth=1.5)
    ax1.set_title(f'{ticker} Price + RSI({rsi_period})', fontsize=14)
    ax1.set_ylabel('Price (USD)')
    ax1.grid(True, alpha=0.3)

    ax2.plot(df.index, rsi, color='purple', linewidth=1.2)
    ax2.axhline(y=70, color='red', linestyle='--', alpha=0.7, label='Overbought (70)')
    ax2.axhline(y=30, color='green', linestyle='--', alpha=0.7, label='Oversold (30)')
    ax2.fill_between(df.index, 70, 100, alpha=0.1, color='red')
    ax2.fill_between(df.index, 0, 30, alpha=0.1, color='green')
    ax2.set_ylabel('RSI')
    ax2.set_ylim(0, 100)
    ax2.legend()
    ax2.grid(True, alpha=0.3)

    plt.tight_layout()
    filename = f"{ticker.lower()}_rsi.png"
    plt.savefig(filename, dpi=150)
    print(f"__FILE__:{filename}")
    current_rsi = rsi.iloc[-1]
    print(f"Current RSI: {current_rsi:.1f}")
    if current_rsi > 70:
        print("Status: OVERBOUGHT ⚠️")
    elif current_rsi < 30:
        print("Status: OVERSOLD 🟢")
    else:
        print("Status: NEUTRAL")
`,
}

// ─── Skill 8: 布林带 ───────────────────────────────────────────────

var skillTechnicalBollinger = Skill{
	Name:        "bollinger_bands",
	Description: "计算并绘制布林带（Bollinger Bands）",
	Keywords:    []string{"bollinger", "布林", "布林带", "波动率", "标准差", "bands"},
	APITips: `1. 中轨 = MA20, 上轨 = MA20 + 2*std, 下轨 = MA20 - 2*std
2. 用 rolling(20).mean() 和 rolling(20).std() 计算`,
	Template: `import yfinance as yf
import matplotlib.pyplot as plt
import pandas as pd

ticker = "AAPL"
period = "6mo"
bb_period = 20
bb_std = 2

df = yf.download(ticker, period=period)
if isinstance(df.columns, pd.MultiIndex):
    df.columns = df.columns.get_level_values(0)

if df.empty:
    print(f"Error: No data for {ticker}")
else:
    close = df['Close']
    sma = close.rolling(window=bb_period).mean()
    std = close.rolling(window=bb_period).std()
    upper = sma + bb_std * std
    lower = sma - bb_std * std

    plt.figure(figsize=(14, 8))
    plt.plot(df.index, close, label='Close', linewidth=1.2)
    plt.plot(df.index, sma, label=f'SMA{bb_period}', linewidth=1, linestyle='--')
    plt.plot(df.index, upper, label='Upper Band', color='red', linewidth=0.8, alpha=0.7)
    plt.plot(df.index, lower, label='Lower Band', color='green', linewidth=0.8, alpha=0.7)
    plt.fill_between(df.index, upper, lower, alpha=0.1, color='blue')
    plt.title(f'{ticker} Bollinger Bands (SMA{bb_period}, {bb_std}σ)', fontsize=14)
    plt.ylabel('Price (USD)')
    plt.legend()
    plt.grid(True, alpha=0.3)
    plt.tight_layout()
    filename = f"{ticker.lower()}_bollinger.png"
    plt.savefig(filename, dpi=150)
    print(f"__FILE__:{filename}")
    print(f"Current price: ${close.iloc[-1]:.2f}")
    print(f"Upper band: ${upper.iloc[-1]:.2f}")
    print(f"Lower band: ${lower.iloc[-1]:.2f}")
    bandwidth = (upper.iloc[-1] - lower.iloc[-1]) / sma.iloc[-1] * 100
    print(f"Bandwidth: {bandwidth:.1f}%")
`,
}

// ─── Skill 9: 数据摘要（表格输出） ─────────────────────────────────

var skillDataSummary = Skill{
	Name:        "data_summary",
	Description: "获取股票关键数据并输出结构化摘要（涨跌幅、成交量等）",
	Keywords:    []string{"数据", "摘要", "summary", "统计", "涨跌幅", "成交量", "volume", "查一下", "查询", "股价"},
	APITips: `1. yfinance.download() 返回 MultiIndex，必须展平
2. 用 pct_change() 计算涨跌幅
3. 打印结构化文本，不需要图表`,
	Template: `import yfinance as yf
import pandas as pd

ticker = "AAPL"
period = "1mo"

df = yf.download(ticker, period=period)
if isinstance(df.columns, pd.MultiIndex):
    df.columns = df.columns.get_level_values(0)

if df.empty:
    print(f"Error: No data returned for {ticker}")
else:
    latest = df.iloc[-1]
    prev = df.iloc[-2] if len(df) > 1 else latest
    first = df.iloc[0]

    daily_change = (latest['Close'] - prev['Close']) / prev['Close'] * 100
    period_change = (latest['Close'] - first['Close']) / first['Close'] * 100
    high_52w = df['High'].max()
    low_52w = df['Low'].min()
    avg_volume = df['Volume'].mean()

    print(f"=== {ticker} Data Summary ({period}) ===")
    print(f"Date range: {df.index[0].strftime('%Y-%m-%d')} to {df.index[-1].strftime('%Y-%m-%d')}")
    print(f"Trading days: {len(df)}")
    print(f"\n--- Latest ---")
    print(f"Close: ${latest['Close']:.2f}")
    print(f"Open:  ${latest['Open']:.2f}")
    print(f"High:  ${latest['High']:.2f}")
    print(f"Low:   ${latest['Low']:.2f}")
    print(f"Volume: {latest['Volume']:,.0f}")
    print(f"\n--- Performance ---")
    print(f"Daily change: {daily_change:+.2f}%")
    print(f"Period change: {period_change:+.2f}%")
    print(f"Period high: ${high_52w:.2f}")
    print(f"Period low:  ${low_52w:.2f}")
    print(f"Avg daily volume: {avg_volume:,.0f}")
`,
}
