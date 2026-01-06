import json
import re

data = {'id': 'gen-1767705374-PIOCc2JNheGlxLZXYea0', 'provider': 'Google', 'model': 'anthropic/claude-sonnet-4.5', 'object': 'chat.completion', 'created': 1767705374, 'choices': [{'logprobs': None, 'finish_reason': 'stop', 'native_finish_reason': 'stop', 'index': 0, 'message': {'role': 'assistant', 'content': '```json\n{\n  "chart_a_analysis": "BEARISH DOMINANCE - The RAG Pattern Analysis shows a clear historical precedent: 6/9 matches (66.7%) trended DOWN with an average future slope of -0.000980. The projected dashed lines beyond the vertical \'Now\' line are PREDOMINANTLY RED, indicating historical paths moved downward. Only 3/9 (33.3%) trended UP. The black \'Current Market\' line shows a recent bounce from around -0.050 to near 0.000, but the historical colored overlays suggest this bounce may exhaust. This is NOT a clean bullish setup - the spaghetti of lines shows mixed short-term behavior, but the forward projection consensus is bearish.",\n  \n  "chart_b_analysis": "CHART B UNAVAILABLE/CORRUPTED - The second image appears to be a solid dark navy/black rectangle with no visible candlestick data, price action, wicks, volume bars, or moving averages. This is a critical data gap. Without Chart B, I cannot detect immediate weakness (shooting stars, bearish engulfing), confirm trend continuation, or identify divergence between macro pattern and micro price action. The absence of live candlestick confirmation is a RED FLAG for trade execution.",\n  \n  "synthesis": "CRITICAL DIVERGENCE/DATA FAILURE - Chart A provides a statistically bearish thesis (66.7% DOWN matches, negative average slope). However, Chart B is non-functional, preventing validation of the immediate price environment. This violates Decision Rule #1: I cannot assess if current candles show rejection wicks or bullish divergence that would contradict the historical bearish bias. Additionally, while Chart A leans bearish, the 33.3% UP matches and recent bounce in the black line introduce uncertainty - this is borderline \'Spaghetti Rule\' territory (not >70% consensus). WITHOUT Chart B confirmation, executing a SHORT trade is RECKLESS.",\n  \n  "signal": "HOLD",\n  \n  "confidence": 0.15\n}\n```\n\n**RISK MANAGER OVERRIDE JUSTIFICATION:**\n\n1. **Data Integrity Failure:** Chart B (Candlestick) is unreadable. Trading blind on micro price action is gambling, not risk management.\n\n2. **Insufficient Consensus:** 66.7% bearish historical matches falls SHORT of the required >70% threshold for high-conviction trades.\n\n3. **Immediate Return Volatility:** Historical matches show erratic immediate returns (ranging from +0.321% to -0.347%), suggesting the pattern\'s predictive power for *entry timing* is weak, even if the eventual trend is down.\n\n4. **Conservative Posture:** The black line\'s recent recovery from -0.050 to 0.000 could be a bear flag OR a genuine reversal. Without candlestick confirmation (e.g., lower highs, weak volume), I cannot differentiate.\n\n**RECOMMENDATION:** Wait for Chart B data refresh OR for historical consensus to exceed 70% with clearer forward projections before risking capital. The -0.000980 slope suggests a mild bearish drift, not a crash - patience is warranted.', 'refusal': None, 'reasoning': None}}], 'usage': {'prompt_tokens': 2530, 'completion_tokens': 724, 'total_tokens': 3254, 'cost': 0.01845, 'is_byok': False, 'prompt_tokens_details': {'cached_tokens': 0, 'cache_write_tokens': 0, 'audio_tokens': 0, 'video_tokens': 0}, 'cost_details': {'upstream_inference_cost': None, 'upstream_inference_prompt_cost': 0.00759, 'upstream_inference_completions_cost': 0.01086}, 'completion_tokens_details': {'reasoning_tokens': 0, 'image_tokens': 0}}}
model = "anthropic/claude-sonnet-4.5"
def extract_content(response: json):
    if model in [
        "google/gemini-2.5-flash", 
        "openai/gpt-4o",
        "anthropic/claude-sonnet-4.5",
    ]:
        # Structure response
        content = response['choices'][0]['message']['content']
        content = re.sub(r"```json\n|\n```", "", content).strip()
        print(content)
        content = json.loads(content)
    
    elif model in [
        "anthropic/claude-3.5-sonnet", 
    ]:
        content = response['choices'][0]['message']['content']
        content = json.loads(content)
    return content

result = extract_content(data)
# print(result)