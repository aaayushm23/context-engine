#!/bin/bash
# demo-fast.sh — Demo with forced rule-based fallback (sub-300ms)
# Use this in the interview for predictable, fast results.
# Then optionally show LLM path with regular demo.sh

ENDPOINT="http://localhost:8080/v1/recommend"
REQUEST_ID="demo-fast-$(date +%s)"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  context-engine  —  production mode demo"
echo "  (rule-based path — predictable latency)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  POST /v1/recommend"
echo "  Location: Berlin Mitte (52.52, 13.405)"
echo "  Preferences: fitness, food"
echo ""

START_MS=$(python3 -c 'import time; print(int(time.time()*1000))')

RESPONSE=$(curl -s -X POST "$ENDPOINT" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: $REQUEST_ID" \
  -H "X-Force-Rules: true" \
  -d '{"lat": 52.52, "lon": 13.405, "available_hours": 3, "preferences": ["fitness", "food"]}')

END_MS=$(python3 -c 'import time; print(int(time.time()*1000))')
WALL_MS=$((END_MS - START_MS))

if [ -z "$RESPONSE" ]; then
  echo "  ERROR: No response from server."
  exit 1
fi

TITLE=$(echo "$RESPONSE" | jq -r '.recommendation.title')
SOURCE=$(echo "$RESPONSE" | jq -r '.recommendation.source')
SIGNALS_USED=$(echo "$RESPONSE" | jq -r '.recommendation.context_signals_used | join(", ")')
SIGNALS_FAILED=$(echo "$RESPONSE" | jq -r 'if (.recommendation.context_signals_failed | length) > 0 then (.recommendation.context_signals_failed | join(", ")) else "none" end')
EXP_COUNT=$(echo "$RESPONSE" | jq '.recommendation.experiences | length')

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

echo "  ✓ Rule-based recommendation (deterministic)"
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
echo "  Architecture: event-driven · resilient · stateless"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
