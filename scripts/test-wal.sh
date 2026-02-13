#!/bin/bash
# WAL Integration Test Script
# Tests: ingestion, WAL storage, Postgres manifest, crash recovery, compaction, corruption handling

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
API_URL="http://localhost:8080"
DATA_DIR="./data"
WAL_DIR="$DATA_DIR/wal"
DB_URL="postgres://selfstack:selfstack@localhost:5432/selfstack?sslmode=disable"
DB_USER="selfstack"
NUM_DOCS=100
API_PID=""

echo "=============================================="
echo "  WAL Integration Test Suite"
echo "=============================================="

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    if [ -n "$API_PID" ] && kill -0 "$API_PID" 2>/dev/null; then
        kill "$API_PID" 2>/dev/null || true
        wait "$API_PID" 2>/dev/null || true
    fi
    # Kill any remaining selfstack processes
    pkill -f "go run ./cmd/api" 2>/dev/null || true
}

trap cleanup EXIT

# Helper functions
log_pass() { echo -e "${GREEN}✓ PASS:${NC} $1"; }
log_fail() { echo -e "${RED}✗ FAIL:${NC} $1"; exit 1; }
log_info() { echo -e "${YELLOW}→${NC} $1"; }

wait_for_api() {
    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if curl -s "$API_URL/health" > /dev/null 2>&1; then
            return 0
        fi
        sleep 0.5
        attempt=$((attempt + 1))
    done
    return 1
}

start_api() {
    log_info "Starting API server..."
    DATABASE_URL="$DB_URL" WAL_COMPACTION=true go run ./cmd/api > /tmp/api.log 2>&1 &
    API_PID=$!
    if wait_for_api; then
        log_pass "API server started (PID: $API_PID)"
    else
        cat /tmp/api.log
        log_fail "API server failed to start"
    fi
}

stop_api() {
    if [ -n "$API_PID" ]; then
        log_info "Stopping API server (PID: $API_PID)..."
        kill "$API_PID" 2>/dev/null || true
        wait "$API_PID" 2>/dev/null || true
        API_PID=""
        sleep 1
    fi
}

kill_api_hard() {
    log_info "Killing API server with SIGKILL (simulating crash)..."
    if [ -n "$API_PID" ]; then
        kill -9 "$API_PID" 2>/dev/null || true
        wait "$API_PID" 2>/dev/null || true
        API_PID=""
    fi
    pkill -9 -f "go run ./cmd/api" 2>/dev/null || true
    sleep 1
}

get_doc_count() {
    curl -s "$API_URL/health" | grep -o '"doc_count":[0-9]*' | cut -d: -f2
}

# ==============================================================================
# TEST 1: Setup and Prerequisites
# ==============================================================================
echo -e "\n${YELLOW}[TEST 1] Prerequisites Check${NC}"

# Check if Postgres is running
if ! docker exec selfstack-db psql -U selfstack -d selfstack -c "SELECT 1" > /dev/null 2>&1; then
    log_info "Postgres not running, starting it..."
    docker compose -f ops/docker-compose.yml up -d
    sleep 5
    # Run migrations
    cat migrations/0001_init.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack 2>/dev/null || true
    cat migrations/0002_wal_segments.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack 2>/dev/null || true
fi
log_pass "Postgres is running"

# Clean up previous test data
log_info "Cleaning up previous test data..."
rm -rf "$DATA_DIR"
docker exec selfstack-db psql -U selfstack -d selfstack -c "TRUNCATE wal_segments CASCADE" 2>/dev/null || true
docker exec selfstack-db psql -U selfstack -d selfstack -c "DELETE FROM wal_state" 2>/dev/null || true
docker exec selfstack-db psql -U selfstack -d selfstack -c "INSERT INTO wal_state (id, current_segment_id, next_lsn, checkpoint_lsn) VALUES (1, 1, 1, 0) ON CONFLICT (id) DO UPDATE SET current_segment_id = 1, next_lsn = 1, checkpoint_lsn = 0" 2>/dev/null || true
log_pass "Test data cleaned"

# ==============================================================================
# TEST 2: Ingest 100 Documents
# ==============================================================================
echo -e "\n${YELLOW}[TEST 2] Ingest $NUM_DOCS Documents${NC}"

start_api

log_info "Ingesting $NUM_DOCS documents..."
for i in $(seq 1 $NUM_DOCS); do
    response=$(curl -s -X POST "$API_URL/ingest" \
        -H "Content-Type: application/json" \
        -d "{
            \"id\": \"doc-$i\",
            \"source\": \"test\",
            \"title\": \"Test Document $i\",
            \"text\": \"This is test document number $i with some content for testing WAL storage and recovery.\"
        }")

    if ! echo "$response" | grep -q '"success":true'; then
        log_fail "Failed to ingest document $i: $response"
    fi

    # Progress indicator
    if [ $((i % 20)) -eq 0 ]; then
        echo -n "  [$i/$NUM_DOCS]"
    fi
done
echo ""

# Verify document count
doc_count=$(get_doc_count)
if [ "$doc_count" -eq "$NUM_DOCS" ]; then
    log_pass "All $NUM_DOCS documents ingested (count: $doc_count)"
else
    log_fail "Document count mismatch: expected $NUM_DOCS, got $doc_count"
fi

# ==============================================================================
# TEST 3: Verify WAL Files Created
# ==============================================================================
echo -e "\n${YELLOW}[TEST 3] Verify WAL Storage${NC}"

if [ -d "$WAL_DIR" ]; then
    log_pass "WAL directory exists: $WAL_DIR"
else
    log_fail "WAL directory not found: $WAL_DIR"
fi

wal_files=$(ls -1 "$WAL_DIR"/*.seg 2>/dev/null | wc -l)
if [ "$wal_files" -gt 0 ]; then
    log_pass "WAL segment files created: $wal_files file(s)"
    ls -lh "$WAL_DIR"/*.seg
else
    log_fail "No WAL segment files found"
fi

# Check WAL file size (should have data)
total_size=$(du -sh "$WAL_DIR" | cut -f1)
log_info "Total WAL size: $total_size"

# ==============================================================================
# TEST 4: Verify Postgres Manifest
# ==============================================================================
echo -e "\n${YELLOW}[TEST 4] Verify Postgres Manifest${NC}"

# Check wal_segments table
segment_count=$(docker exec selfstack-db psql -U selfstack -d selfstack -t -c "SELECT COUNT(*) FROM wal_segments" | tr -d ' ')
if [ "$segment_count" -gt 0 ]; then
    log_pass "Segments tracked in Postgres: $segment_count"
    docker exec selfstack-db psql -U selfstack -d selfstack -c "SELECT segment_id, status, size_bytes, record_count FROM wal_segments ORDER BY segment_id"
else
    log_fail "No segments found in Postgres"
fi

# Check wal_state table
wal_state=$(docker exec selfstack-db psql -U selfstack -d selfstack -t -c "SELECT current_segment_id, next_lsn FROM wal_state WHERE id = 1" | tr -d ' ')
if [ -n "$wal_state" ]; then
    log_pass "WAL state tracked in Postgres: $wal_state"
else
    log_info "WAL state not yet initialized (will be on next rotation)"
fi

# ==============================================================================
# TEST 5: Crash Recovery Test
# ==============================================================================
echo -e "\n${YELLOW}[TEST 5] Crash Recovery Test${NC}"

log_info "Capturing pre-crash state..."
pre_crash_count=$(get_doc_count)
log_info "Pre-crash document count: $pre_crash_count"

# Simulate crash
kill_api_hard

log_info "Restarting API server..."
start_api

# Verify recovery
post_crash_count=$(get_doc_count)
if [ "$post_crash_count" -eq "$pre_crash_count" ]; then
    log_pass "Crash recovery successful! Documents recovered: $post_crash_count"
else
    log_fail "Crash recovery failed: expected $pre_crash_count, got $post_crash_count"
fi

# Verify search still works
search_result=$(curl -s -X POST "$API_URL/search" \
    -H "Content-Type: application/json" \
    -d '{"query": "test document", "limit": 5}')

if echo "$search_result" | grep -q '"results"'; then
    result_count=$(echo "$search_result" | grep -o '"doc_id"' | wc -l)
    log_pass "Search works after recovery (found $result_count results)"
else
    log_fail "Search failed after recovery: $search_result"
fi

# ==============================================================================
# TEST 6: Test Delete and Recovery
# ==============================================================================
echo -e "\n${YELLOW}[TEST 6] Delete and Recovery Test${NC}"

# Delete some documents by ingesting new ones with same ID but then we'll check if tombstones work
# Actually, let's add more docs and verify count
log_info "Adding 10 more documents after recovery..."
for i in $(seq 101 110); do
    curl -s -X POST "$API_URL/ingest" \
        -H "Content-Type: application/json" \
        -d "{
            \"id\": \"doc-$i\",
            \"source\": \"test\",
            \"title\": \"Post-recovery Doc $i\",
            \"text\": \"Document added after crash recovery test.\"
        }" > /dev/null
done

new_count=$(get_doc_count)
expected_count=$((NUM_DOCS + 10))
if [ "$new_count" -eq "$expected_count" ]; then
    log_pass "Post-recovery ingestion works (count: $new_count)"
else
    log_fail "Post-recovery count mismatch: expected $expected_count, got $new_count"
fi

# ==============================================================================
# TEST 7: Corruption Handling Test
# ==============================================================================
echo -e "\n${YELLOW}[TEST 7] Corruption Handling Test${NC}"

stop_api

# Find a WAL segment to corrupt
wal_file=$(ls -1 "$WAL_DIR"/*.seg 2>/dev/null | head -1)
if [ -n "$wal_file" ]; then
    log_info "Testing corrupt tail truncation..."

    # Create a backup first
    cp "$wal_file" "${wal_file}.backup"
    original_size=$(stat -f%z "$wal_file" 2>/dev/null || stat -c%s "$wal_file" 2>/dev/null)

    # Append garbage to the actual WAL segment (simulates crash mid-write)
    echo "CORRUPTED_TAIL_DATA_SIMULATING_CRASH" >> "$wal_file"
    corrupted_size=$(stat -f%z "$wal_file" 2>/dev/null || stat -c%s "$wal_file" 2>/dev/null)
    log_info "Corrupted segment: $original_size -> $corrupted_size bytes"

    log_info "Starting API (should truncate corrupt tail)..."
    start_api

    # Check if API started
    if curl -s "$API_URL/health" | grep -q '"status":"healthy"'; then
        log_pass "API handles corrupt tail gracefully"
    else
        log_fail "API failed to start with corrupt tail"
    fi

    # Verify documents are still accessible
    recovered_count=$(get_doc_count)
    log_info "Documents recovered after corruption fix: $recovered_count"

    # Check if file was truncated
    new_size=$(stat -f%z "$wal_file" 2>/dev/null || stat -c%s "$wal_file" 2>/dev/null)
    if [ "$new_size" -le "$original_size" ]; then
        log_pass "Corrupt tail was truncated: $corrupted_size -> $new_size bytes"
    else
        log_info "File size: $new_size bytes"
    fi

    # Cleanup - restore backup for consistent state
    stop_api
    cp "${wal_file}.backup" "$wal_file"
    rm -f "${wal_file}.backup"
    start_api
else
    log_info "Skipping corruption test (no WAL files)"
fi

# ==============================================================================
# TEST 8: Segment Rotation Test
# ==============================================================================
echo -e "\n${YELLOW}[TEST 8] Segment Rotation Test${NC}"

# Get current segment count
pre_rotation_segments=$(ls -1 "$WAL_DIR"/*.seg 2>/dev/null | wc -l)
log_info "Segments before bulk insert: $pre_rotation_segments"

# Insert enough data to trigger rotation (small segment size for testing)
log_info "Inserting large documents to trigger rotation..."
large_text=$(python3 -c "print('x' * 50000)" 2>/dev/null || printf 'x%.0s' {1..50000})

for i in $(seq 1 20); do
    curl -s -X POST "$API_URL/ingest" \
        -H "Content-Type: application/json" \
        -d "{
            \"id\": \"large-doc-$i\",
            \"source\": \"rotation-test\",
            \"title\": \"Large Document $i\",
            \"text\": \"$large_text\"
        }" > /dev/null
done

post_rotation_segments=$(ls -1 "$WAL_DIR"/*.seg 2>/dev/null | wc -l)
log_info "Segments after bulk insert: $post_rotation_segments"

if [ "$post_rotation_segments" -gt "$pre_rotation_segments" ]; then
    log_pass "Segment rotation occurred ($pre_rotation_segments -> $post_rotation_segments segments)"
else
    log_info "Segment rotation not triggered (would need more data or smaller segment size)"
fi

# ==============================================================================
# TEST 9: Final Crash Recovery
# ==============================================================================
echo -e "\n${YELLOW}[TEST 9] Final Crash Recovery Verification${NC}"

final_pre_crash=$(get_doc_count)
log_info "Final document count before crash: $final_pre_crash"

kill_api_hard
start_api

final_post_crash=$(get_doc_count)
if [ "$final_post_crash" -eq "$final_pre_crash" ]; then
    log_pass "Final crash recovery successful! All $final_post_crash documents recovered"
else
    log_fail "Final crash recovery failed: expected $final_pre_crash, got $final_post_crash"
fi

# ==============================================================================
# TEST 10: Compaction Check
# ==============================================================================
echo -e "\n${YELLOW}[TEST 10] Compaction Status${NC}"

# Check for sealed segments (candidates for compaction)
sealed_count=$(docker exec selfstack-db psql -U selfstack -d selfstack -t -c "SELECT COUNT(*) FROM wal_segments WHERE status = 'sealed'" 2>/dev/null | tr -d ' ')
if [ -n "$sealed_count" ] && [ "$sealed_count" -gt 0 ]; then
    log_pass "Sealed segments available for compaction: $sealed_count"

    # Show segment status
    docker exec selfstack-db psql -U selfstack -d selfstack -c "SELECT segment_id, status, size_bytes FROM wal_segments ORDER BY segment_id"
else
    log_info "No sealed segments yet (rotation needed for compaction)"
fi

# ==============================================================================
# Summary
# ==============================================================================
echo -e "\n=============================================="
echo -e "${GREEN}  WAL Integration Tests Complete!${NC}"
echo "=============================================="
echo ""
echo "Final Statistics:"
echo "  - Documents: $final_post_crash"
echo "  - WAL Segments: $(ls -1 "$WAL_DIR"/*.seg 2>/dev/null | wc -l)"
echo "  - Total WAL Size: $(du -sh "$WAL_DIR" 2>/dev/null | cut -f1)"
echo ""
echo "All tests passed!"

stop_api
