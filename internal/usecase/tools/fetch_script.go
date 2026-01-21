package tools

// ScriptFetchFundamentals Python 脚本模板
// 升级版：使用 yahooquery 替代 yfinance 获取简介，大幅提升成功率
const ScriptFetchFundamentals = `
import json
import sys
from yahooquery import Ticker
import yfinance as yf

ticker_symbol = "{{.Ticker}}"
print(f"Fetching data for {ticker_symbol} using yahooquery...")

# 初始化 YahooQuery Ticker
yq_ticker = Ticker(ticker_symbol)

# 1. 获取 Asset Profile (公司简介、行业)
# yahooquery 会返回一个字典: {'NVDA': {'longBusinessSummary': '...', ...}}
try:
    # 尝试获取公司画像 (适用于股票)
    profile = yq_ticker.asset_profile
    summary_data = profile.get(ticker_symbol, {})
    
    # 如果是字符串 (错误信息)，说明可能不是普通公司，尝试作为 ETF 获取
    if isinstance(summary_data, str):
         # 尝试获取基金画像 (适用于 ETF，如 VOO)
         # 注意：ETF 的简介通常在 summary_profile 或 fund_profile 中
         summary_data = yq_ticker.summary_profile.get(ticker_symbol, {})

    # 提取字段
    summary = summary_data.get('longBusinessSummary', None)
    
    # 如果还是没拿到，尝试 ETF 的特殊字段
    if not summary:
        summary = summary_data.get('longDescription', 'No summary available.')

    sector = summary_data.get('sector', 'Unknown')
    industry = summary_data.get('industry', 'Unknown')
    
    # ETF 特有字段
    if sector == 'Unknown':
        sector = summary_data.get('categoryName', 'ETF/Fund')

except Exception as e:
    print(f"[ERROR] YahooQuery failed: {e}")
    summary = "Data fetch failed."
    sector = "Error"
    industry = "Error"

# 2. 构造文本内容
content = f"""
Symbol: {ticker_symbol}
Sector: {sector}
Industry: {industry}
Business Summary:
{summary}
"""

# 3. 简单的质量检查
# 如果内容太短，可能是被反爬了，打印警告到 stderr (会被 Go 捕获)
if len(summary) < 50:
    print(f"[WARNING] Summary is too short, possibly rate limited or invalid ticker.", file=sys.stderr)

# 4. 保存文件
filename = f"{ticker_symbol}_profile.txt"
with open(filename, "w", encoding="utf-8") as f:
    f.write(content)

# 5. 打印协议头
print(f"__FILE__:{filename}")
print(f"Successfully processed {ticker_symbol}")
`
