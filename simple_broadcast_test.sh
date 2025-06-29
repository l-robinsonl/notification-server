#!/bin/bash

# Enhanced WebSocket Integration Test with Beautiful Output
# Creates fake WebSocket clients, sends messages, validates results, then cleans up

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Configuration
API_KEY="${TF_NOTI_SECRET:-lD8Z0Nu+Afezs+jQugR+B59klTtmFDlv+xh225oAwhs=}"
API_URL="http://localhost:8081"
WS_URL="ws://localhost:8081/ws"
TEST_DIR="/tmp/ws_integration_test_$"
LOG_FILE="$TEST_DIR/test.log"

# Check if API key is set
if [[ -z "$TF_NOTI_SECRET" ]]; then
    echo -e "${YELLOW}⚠️  Using default API key (TF_NOTI_SECRET not set)${NC}"
else
    echo -e "${GREEN}✅ Using API key from TF_NOTI_SECRET environment variable${NC}"
fi

# Test tracking
TESTS_PASSED=0
TESTS_FAILED=0
TOTAL_TESTS=0

# Create test directory
mkdir -p "$TEST_DIR"

# Enhanced banner
echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}               ${CYAN}${BOLD}WebSocket Integration Test Suite${NC}               ${BLUE}║${NC}"
echo -e "${BLUE}║${NC}          ${YELLOW}Testing Real-World Message Broadcasting${NC}           ${BLUE}║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Enhanced WebSocket client script with message tracking
cat > "$TEST_DIR/ws_client.py" << 'EOF'
#!/usr/bin/env python3
import asyncio
import websockets
import json
import sys
import signal
import time
import os

class FakeClient:
    def __init__(self, user_id, team_id, test_dir):
        self.user_id = user_id
        self.team_id = team_id
        self.test_dir = test_dir
        self.running = True
        self.messages_received = []
        self.log_file = f"{test_dir}/received_{user_id}.json"
        
        # Clear previous log
        if os.path.exists(self.log_file):
            os.remove(self.log_file)
        
    def log_message(self, data):
        """Log received message to file for validation"""
        message_info = {
            "receiver": self.user_id,
            "team": self.team_id,
            "message_type": data.get('messageType', 'unknown'),
            "body": data.get('body', ''),
            "notification_id": data.get('notificationId', ''),
            "timestamp": time.time()
        }
        
        with open(self.log_file, "a") as f:
            f.write(json.dumps(message_info) + "\n")
        
        return message_info
        
    async def connect_and_listen(self, ws_url):
        try:
            print(f"🔗 Connecting {self.user_id} (team {self.team_id})...")
            async with websockets.connect(ws_url) as websocket:
                # Send auth message with fake token
                auth_msg = {
                    "type": "auth",
                    "userId": self.user_id,
                    "teamId": self.team_id,
                    "token": "fake_development_token"
                }
                
                await websocket.send(json.dumps(auth_msg))
                
                # Listen for messages
                while self.running:
                    try:
                        message = await asyncio.wait_for(websocket.recv(), timeout=1.0)
                        data = json.loads(message)
                        
                        if data.get("type") == "auth_success":
                            print(f"✅ {self.user_id} authenticated successfully")
                        elif data.get("type") != "ping":  # Filter out ping messages
                            msg_info = self.log_message(data)
                            msg_type = msg_info['message_type']
                            body = msg_info['body'][:50] + "..." if len(msg_info['body']) > 50 else msg_info['body']
                            print(f"📨 {self.user_id} received [{msg_type}]: {body}")
                            
                    except asyncio.TimeoutError:
                        continue
                    except websockets.exceptions.ConnectionClosed:
                        print(f"🔌 {self.user_id} connection closed")
                        break
                        
        except Exception as e:
            print(f"❌ {self.user_id} connection failed: {e}")
    
    def stop(self):
        self.running = False

async def main():
    user_id = sys.argv[1]
    team_id = sys.argv[2]
    ws_url = sys.argv[3]
    test_dir = sys.argv[4]
    
    client = FakeClient(user_id, team_id, test_dir)
    
    # Handle signals gracefully
    def signal_handler(sig, frame):
        client.stop()
    
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    await client.connect_and_listen(ws_url)

if __name__ == "__main__":
    asyncio.run(main())
EOF

chmod +x "$TEST_DIR/ws_client.py"

# Function to debug server response
debug_server_response() {
    local test_name="$1"
    local curl_data="$2"
    
    echo -e "${CYAN}   🔍 Debugging server response for $test_name:${NC}"
    
    local response=$(curl -s -X POST "$API_URL/send" \
        -H "X-API-Key: $API_KEY" \
        -H "Content-Type: application/json" \
        -d "$curl_data")
    
    echo -e "${CYAN}      Raw response: '$response'${NC}"
    echo -e "${CYAN}      Response length: ${#response}${NC}"
    
    if [[ ${#response} -gt 0 ]]; then
        echo -e "${CYAN}      First 20 chars: '${response:0:20}'${NC}"
        
        # Try to parse with jq
        if echo "$response" | jq '.' >/dev/null 2>&1; then
            echo -e "${GREEN}      ✅ Valid JSON${NC}"
        else
            echo -e "${RED}      ❌ Invalid JSON - this is the parsing issue!${NC}"
            echo -e "${YELLOW}      Hex dump of first few bytes:${NC}"
            echo "$response" | head -c 50 | xxd | head -2 | sed 's/^/         /'
        fi
    else
        echo -e "${RED}      ❌ Empty response${NC}"
    fi
}

# Function to run a test
run_test() {
    local test_name="$1"
    local expected_description="$2"
    local curl_data="$3"
    local validation_function="$4"
    
    ((TOTAL_TESTS++))
    
    echo -e "${PURPLE}🧪 Test: ${BOLD}$test_name${NC}"
    echo -e "${CYAN}   Expected: $expected_description${NC}"
    
    # Debug the server response first
    debug_server_response "$test_name" "$curl_data"
    
    # Send the message (without jq to avoid parsing errors)
    local response=$(curl -s -X POST "$API_URL/send" \
        -H "X-API-Key: $API_KEY" \
        -H "Content-Type: application/json" \
        -d "$curl_data")
    
    echo -e "${YELLOW}   📡 Message sent... Response: $response${NC}"
    sleep 3  # Allow more time for message propagation
    
    # Show what we received for debugging
    echo -e "${CYAN}   📄 Log files status:${NC}"
    for file in "$TEST_DIR"/received_*.json; do
        if [[ -f "$file" ]]; then
            local count=$(wc -l < "$file")
            local filename=$(basename "$file")
            echo -e "${CYAN}      $filename: $count messages${NC}"
        fi
    done
    
    # Validate results
    if $validation_function; then
        echo -e "${GREEN}   ✅ PASS${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}   ❌ FAIL${NC}"
        echo -e "${YELLOW}   📋 Showing recent log contents for debugging:${NC}"
        for file in "$TEST_DIR"/received_*.json; do
            if [[ -f "$file" ]]; then
                echo -e "${YELLOW}      $(basename "$file"):${NC}"
                tail -3 "$file" | sed 's/^/         /'
            fi
        done
        ((TESTS_FAILED++))
    fi
    echo ""
}

# Validation functions with debug output
validate_team1_broadcast() {
    local user1_received=$(grep -c "Team 1 only" "$TEST_DIR/received_user_1_1.json" 2>/dev/null || echo "0")
    local user2_received=$(grep -c "Team 1 only" "$TEST_DIR/received_user_1_2.json" 2>/dev/null || echo "0")
    local user3_received=$(grep -c "Team 1 only" "$TEST_DIR/received_user_2_1.json" 2>/dev/null || echo "0")
    
    # Clean variables to remove any whitespace/newlines
    user1_received=$(echo "$user1_received" | tr -d '\n\r ' | head -c 3)
    user2_received=$(echo "$user2_received" | tr -d '\n\r ' | head -c 3)
    user3_received=$(echo "$user3_received" | tr -d '\n\r ' | head -c 3)
    
    echo -e "${CYAN}   Debug: user_1_1=$user1_received, user_1_2=$user2_received, user_2_1=$user3_received${NC}"
    
    [[ "$user1_received" -gt 0 ]] && [[ "$user2_received" -gt 0 ]] && [[ "$user3_received" -eq 0 ]]
}

validate_global_broadcast() {
    local user1_received=$(grep -c "GLOBAL ALERT" "$TEST_DIR/received_user_1_1.json" 2>/dev/null || echo "0")
    local user2_received=$(grep -c "GLOBAL ALERT" "$TEST_DIR/received_user_1_2.json" 2>/dev/null || echo "0")
    local user3_received=$(grep -c "GLOBAL ALERT" "$TEST_DIR/received_user_2_1.json" 2>/dev/null || echo "0")
    
    # Clean variables to remove any whitespace/newlines
    user1_received=$(echo "$user1_received" | tr -d '\n\r ' | head -c 3)
    user2_received=$(echo "$user2_received" | tr -d '\n\r ' | head -c 3)
    user3_received=$(echo "$user3_received" | tr -d '\n\r ' | head -c 3)
    
    echo -e "${CYAN}   Debug: user_1_1=$user1_received, user_1_2=$user2_received, user_2_1=$user3_received${NC}"
    
    [[ "$user1_received" -gt 0 ]] && [[ "$user2_received" -gt 0 ]] && [[ "$user3_received" -gt 0 ]]
}

validate_direct_message() {
    local user1_received=$(grep -c "Personal message for user_1_1" "$TEST_DIR/received_user_1_1.json" 2>/dev/null || echo "0")
    local user2_received=$(grep -c "Personal message for user_1_1" "$TEST_DIR/received_user_1_2.json" 2>/dev/null || echo "0")
    local user3_received=$(grep -c "Personal message for user_1_1" "$TEST_DIR/received_user_2_1.json" 2>/dev/null || echo "0")
    
    # Clean variables to remove any whitespace/newlines
    user1_received=$(echo "$user1_received" | tr -d '\n\r ' | head -c 3)
    user2_received=$(echo "$user2_received" | tr -d '\n\r ' | head -c 3)
    user3_received=$(echo "$user3_received" | tr -d '\n\r ' | head -c 3)
    
    echo -e "${CYAN}   Debug: user_1_1=$user1_received, user_1_2=$user2_received, user_2_1=$user3_received${NC}"
    
    [[ "$user1_received" -gt 0 ]] && [[ "$user2_received" -eq 0 ]] && [[ "$user3_received" -eq 0 ]]
}

validate_team2_broadcast() {
    local user1_received=$(grep -c "Team 2 only" "$TEST_DIR/received_user_1_1.json" 2>/dev/null || echo "0")
    local user2_received=$(grep -c "Team 2 only" "$TEST_DIR/received_user_1_2.json" 2>/dev/null || echo "0")
    local user3_received=$(grep -c "Team 2 only" "$TEST_DIR/received_user_2_1.json" 2>/dev/null || echo "0")
    
    # Clean variables to remove any whitespace/newlines
    user1_received=$(echo "$user1_received" | tr -d '\n\r ' | head -c 3)
    user2_received=$(echo "$user2_received" | tr -d '\n\r ' | head -c 3)
    user3_received=$(echo "$user3_received" | tr -d '\n\r ' | head -c 3)
    
    echo -e "${CYAN}   Debug: user_1_1=$user1_received, user_1_2=$user2_received, user_2_1=$user3_received${NC}"
    
    [[ "$user1_received" -eq 0 ]] && [[ "$user2_received" -eq 0 ]] && [[ "$user3_received" -gt 0 ]]
}

# Cleanup function
cleanup() {
    echo -e "${YELLOW}🧹 Cleaning up test environment...${NC}"
    if [[ -n $CLIENT1_PID ]]; then kill $CLIENT1_PID 2>/dev/null; fi
    if [[ -n $CLIENT2_PID ]]; then kill $CLIENT2_PID 2>/dev/null; fi
    if [[ -n $CLIENT3_PID ]]; then kill $CLIENT3_PID 2>/dev/null; fi
    rm -rf "$TEST_DIR"
    echo -e "${GREEN}✨ Cleanup complete!${NC}"
    exit 0  # Force exit after cleanup
}

# Trap signals for cleanup
trap cleanup EXIT INT TERM

# Check if server is running
echo -e "${YELLOW}🔍 Checking if WebSocket server is running...${NC}"
if ! curl -s "$API_URL/health" > /dev/null; then
    echo -e "${RED}❌ WebSocket server is not running at $API_URL${NC}"
    echo -e "${YELLOW}💡 Please start your server first: cd src && go run .${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Server is running${NC}"
echo ""

# Start WebSocket clients
echo -e "${YELLOW}👥 Starting WebSocket clients...${NC}"

python3 "$TEST_DIR/ws_client.py" "user_1_1" "team_1" "$WS_URL" "$TEST_DIR" &
CLIENT1_PID=$!
sleep 1

python3 "$TEST_DIR/ws_client.py" "user_1_2" "team_1" "$WS_URL" "$TEST_DIR" &
CLIENT2_PID=$!
sleep 1

python3 "$TEST_DIR/ws_client.py" "user_2_1" "team_2" "$WS_URL" "$TEST_DIR" &
CLIENT3_PID=$!
sleep 3

echo -e "${GREEN}✅ All clients connected and authenticated${NC}"
echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}                     🚀 Running Tests                      ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Test 1: Team Broadcast
run_test "Team 1 Broadcast" \
         "user_1_1 and user_1_2 receive, user_2_1 does not" \
         '{
            "notification_id": "test_team_broadcast_1",
            "sender_user_id": "system",
            "target_team_id": "team_1",
            "message_type": "system_notification",
            "body": "🎯 Team 1 only: This is a team-specific broadcast!",
            "broadcast": true
          }' \
         validate_team1_broadcast

# Test 2: Global Broadcast
run_test "Global Broadcast" \
         "ALL users receive the message" \
         '{
            "notification_id": "test_global_broadcast",
            "sender_user_id": "system",
            "message_type": "system_alert",
            "body": "🚨 GLOBAL ALERT: This should reach ALL teams!",
            "broadcast": true
          }' \
         validate_global_broadcast

# Test 3: Direct Message
run_test "Direct Message" \
         "Only user_1_1 receives the message" \
         '{
            "notification_id": "test_direct_message",
            "sender_user_id": "system",
            "target_team_id": "team_1",
            "target_user_id": "user_1_1",
            "message_type": "user_message",
            "body": "👋 Personal message for user_1_1 only!",
            "broadcast": false
          }' \
         validate_direct_message

# Test 4: Team 2 Broadcast
run_test "Team 2 Broadcast" \
         "Only user_2_1 receives the message" \
         '{
            "notification_id": "test_team_broadcast_2",
            "sender_user_id": "system",
            "target_team_id": "team_2",
            "message_type": "system_notification",
            "body": "🎯 Team 2 only: This is for team 2 members!",
            "broadcast": true
          }' \
         validate_team2_broadcast

# Final Results
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${YELLOW}📊 INTEGRATION TEST SUMMARY${NC}"
echo -e "${BLUE}────────────────────────────────────────────────────────────${NC}"
echo -e "Tests Passed:   ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests Failed:   ${RED}$TESTS_FAILED${NC}"
echo -e "Total Tests:    ${CYAN}$TOTAL_TESTS${NC}"
echo -e "${BLUE}────────────────────────────────────────────────────────────${NC}"

if [[ $TESTS_FAILED -eq 0 ]]; then
    echo -e "${GREEN}🎉 ALL INTEGRATION TESTS PASSED! 🎉${NC}"
    echo -e "${CYAN}✨ Your WebSocket server is working perfectly!${NC}"
    EXIT_CODE=0
else
    echo -e "${RED}💥 SOME INTEGRATION TESTS FAILED 💥${NC}"
    echo -e "${YELLOW}🔍 Check the test logs in $TEST_DIR/ for details${NC}"
    EXIT_CODE=1
fi

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Don't cleanup immediately if there were failures (for debugging)
if [[ $TESTS_FAILED -gt 0 ]]; then
    echo -e "${YELLOW}💡 Test files preserved in $TEST_DIR for debugging${NC}"
    echo -e "${YELLOW}   Press Ctrl+C to cleanup and exit (or wait 30 seconds for auto-exit)${NC}"
    
    # Keep running for manual inspection but with timeout
    WAIT_TIME=0
    while [[ $WAIT_TIME -lt 30 ]]; do
        sleep 1
        ((WAIT_TIME++))
    done
    
    echo -e "${CYAN}⏰ Auto-exiting after 30 seconds...${NC}"
fi

exit $EXIT_CODE