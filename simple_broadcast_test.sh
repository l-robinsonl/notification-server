#!/bin/bash

# enhanced_broadcast_test.sh
# Creates fake WebSocket clients, sends messages, then cleans up

echo "🚀 Starting Enhanced Broadcast Test"
echo "===================================="

API_KEY="lD8Z0Nu+Afezs+jQugR+B59klTtmFDlv+xh225oAwhs="
API_URL="http://localhost:8081"
WS_URL="ws://localhost:8081/ws"

# Create a simple WebSocket client script
cat > ws_client.py << 'EOF'
#!/usr/bin/env python3
import asyncio
import websockets
import json
import sys
import signal

class FakeClient:
    def __init__(self, user_id, team_id):
        self.user_id = user_id
        self.team_id = team_id
        self.running = True
        
    async def connect_and_listen(self, ws_url):
        try:
            print(f"🔗 Connecting {self.user_id} (team {self.team_id})...")
            async with websockets.connect(ws_url) as websocket:
                # Send auth message with fake token (your server will need to handle this)
                auth_msg = {
                    "type": "auth",
                    "user_id": self.user_id,
                    "team_id": self.team_id,
                    "token": "fake_development_token"
                }
                
                await websocket.send(json.dumps(auth_msg))
                print(f"✅ {self.user_id} authenticated and listening...")
                
                # Listen for messages
                while self.running:
                    try:
                        message = await asyncio.wait_for(websocket.recv(), timeout=1.0)
                        data = json.loads(message)
                        if data.get("type") != "auth_success":
                            print(f"📨 {self.user_id} received: {data.get('messageType', 'unknown')} - {data.get('body', '')}")
                    except asyncio.TimeoutError:
                        continue
                    except websockets.exceptions.ConnectionClosed:
                        break
                        
        except Exception as e:
            print(f"❌ {self.user_id} connection failed: {e}")
    
    def stop(self):
        self.running = False

async def main():
    user_id = sys.argv[1]
    team_id = sys.argv[2]
    ws_url = sys.argv[3]
    
    client = FakeClient(user_id, team_id)
    
    # Handle Ctrl+C gracefully
    def signal_handler(sig, frame):
        print(f"\n🛑 Stopping {user_id}...")
        client.stop()
    
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    await client.connect_and_listen(ws_url)

if __name__ == "__main__":
    asyncio.run(main())
EOF

# Make the Python script executable
chmod +x ws_client.py

echo "👥 Starting fake WebSocket clients..."

# Start fake clients in background
python3 ws_client.py "user_1_1" "team_1" "$WS_URL" &
CLIENT1_PID=$!
sleep 1

python3 ws_client.py "user_1_2" "team_1" "$WS_URL" &
CLIENT2_PID=$!
sleep 1

python3 ws_client.py "user_2_1" "team_2" "$WS_URL" &
CLIENT3_PID=$!
sleep 2

echo "✅ All fake clients connected!"
echo ""

# Function to cleanup clients
cleanup() {
    echo "🧹 Cleaning up WebSocket clients..."
    kill $CLIENT1_PID $CLIENT2_PID $CLIENT3_PID 2>/dev/null
    rm -f ws_client.py
    echo "👋 Cleanup complete!"
}

# Trap Ctrl+C to cleanup
trap cleanup EXIT

echo "📡 Testing Team Broadcast to team_1..."
echo "Expected: user_1_1 and user_1_2 should receive this message"
curl -s -X POST "$API_URL/send" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "notification_id": "test_team_broadcast_1",
    "sender_user_id": "system",
    "target_team_id": "team_1",
    "target_user_id": "",
    "message_type": "system_notification",
    "body": "🎯 Team 1 only: This is a team-specific broadcast!",
    "broadcast": true
  }' | jq '.'

echo ""
sleep 3

echo "🌍 Testing Global Broadcast..."
echo "Expected: ALL users (user_1_1, user_1_2, user_2_1) should receive this"
curl -s -X POST "$API_URL/send" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "notification_id": "test_global_broadcast",
    "sender_user_id": "system",
    "target_team_id": "",
    "target_user_id": "",
    "message_type": "system_alert",
    "body": "🚨 GLOBAL ALERT: This should reach ALL teams!",
    "broadcast": true
  }' | jq '.'

echo ""
sleep 3

echo "📧 Testing Direct Message to user_1_1..."
echo "Expected: Only user_1_1 should receive this message"
curl -s -X POST "$API_URL/send" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "notification_id": "test_direct_message",
    "sender_user_id": "system",
    "target_team_id": "team_1",
    "target_user_id": "user_1_1",
    "message_type": "user_message",
    "body": "👋 Personal message for user_1_1 only!",
    "broadcast": false
  }' | jq '.'

echo ""
sleep 3

echo "📡 Testing Team 2 Broadcast..."
echo "Expected: Only user_2_1 should receive this message"
curl -s -X POST "$API_URL/send" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "notification_id": "test_team_broadcast_2",
    "sender_user_id": "system",
    "target_team_id": "team_2",
    "target_user_id": "",
    "message_type": "system_notification",
    "body": "🎯 Team 2 only: This is for team 2 members!",
    "broadcast": true
  }' | jq '.'

echo ""
echo "⏳ Waiting a moment to see all messages..."
sleep 5

echo ""
echo "✅ Test complete! "
echo "📊 Check the output above to verify:"
echo "   - Team broadcasts went to correct team members only"
echo "   - Global broadcast reached all users"
echo "   - Direct message reached only the target user"
echo ""
echo "Press Ctrl+C to exit and cleanup..."

# Keep script running until user stops it
while true; do
    sleep 1
done