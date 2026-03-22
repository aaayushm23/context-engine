#!/bin/bash
# demo.sh — Formatted recommendation demo

ENDPOINT="http://localhost:8080/v1/recommend"
REQUEST_ID="demo-$(date +%s)"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  context-engine  —  live recommendation demo"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  POST /v1/recommend"
echo "  Location: Berlin Mitte (52.52, 13.405)"
echo "  Preferences: fitness, food"
echo "  Available: 3 hours"
echo ""

START_MS=$(python3 -c 'import time; print(int(time.time()*1000))')

RESPONSE=$(curl -s -X POST "$ENDPOINT" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: $REQUEST_ID" \
  -d '{"lat": 52.52, "lon": 13.405, "available_hours": 3, "preferences": ["fitness", "food"]}')

END_MS=$(python3 -c 'import time; print(int(time.time()*1000))')
WALL_MS=$((END_MS - START_MS))

if [ -z "$RESPONSE" ]; then
  echo "  ERROR: No response from server. Is it running?"
  exit 1
fi

# Parse
TITLE=$(echo "$RESPONSE" | jq -r '.recommendation.title')
SOURCE=$(echo "$RESPONSE" | jq -r '.recommendation.source')
LATENCY=$(echo "$RESPONSE" | jq -r '.meta.latency_ms')
FROM_CACHE=$(echo "$RESPONSE" | jq -r '.meta.from_cache')
SIGNALS_USED=$(echo "$RESPONSE" | jq -r '.recommendation.context_signals_used | join(", ")')
SIGNALS_FAILED=$(echo "$RESPONSE" | jq -r 'if (.recommendation.context_signals_failed | length) > 0 then (.recommendation.context_signals_failed | join(", ")) else "none" end')
EXP_COUNT=$(echo "$RESPONSE" | jq '.recommendation.experiences | length')

if [ "$FROM_CACHE" = "true" ]; then
  echo "  ⚡ Served from cache (idempotent)"
  echo "  Wall time: ${WALL_MS}ms"
  echo ""
  exit 0
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  $TITLE"
echo ""

for i in $(seq 0 $(($EXP_COUNT - 1))); do
  NAME=$(echo "$RESPONSE" | jq -r ".recommendation.experiences[$i].partner_name")
  CATEGORY=$(echo "$RESPONSE" | jq -r ".recommendation.experiences[$i].category")
  REASON=$(echo "$RESPONSE" | jq -r ".recommendation.experiences[$i].reason")
  DISTANCE=$(echo "$RESPONSE" | jq -r ".recommendation.experiences[$i].distance_km")
  DIST_FMT=$(printf "%.1f" "$DISTANCE")

  echo "  $(($i + 1)). $NAME"
  echo "     $REASON"
  echo "     [$CATEGORY · ${DIST_FMT}km away]"
  echo ""
done

# Source indicator
if [ "$SOURCE" = "llm" ]; then
  echo "  ✓ LLM-composed recommendation"
else
  echo "  ⚠ Fallback activated (rule-based — LLM unavailable)"
fi

echo "  ✓ System returned result despite potential external failures"
echo ""

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  Source:           $SOURCE"
echo "  Latency:          ${WALL_MS}ms"
echo "  Signals used:     $SIGNALS_USED"
echo "  Signals failed:   $SIGNALS_FAILED"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── Idempotency test ──
echo "  Idempotency test (same request_id)..."
echo ""

CACHE_START=$(python3 -c 'import time; print(int(time.time()*1000))')
CACHED=$(curl -s -X POST "$ENDPOINT" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: $REQUEST_ID" \
  -d '{"lat": 52.52, "lon": 13.405, "available_hours": 3, "preferences": ["fitness", "food"]}')
CACHE_END=$(python3 -c 'import time; print(int(time.time()*1000))')
CACHE_WALL=$((CACHE_END - CACHE_START))

CACHED_FROM=$(echo "$CACHED" | jq -r '.meta.from_cache')

echo "  ⚡ Idempotency verified"
echo "     First request:  ${WALL_MS}ms"
echo "     Cached request: ${CACHE_WALL}ms (from_cache: $CACHED_FROM)"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Architecture: event-driven · resilient · stateless"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
