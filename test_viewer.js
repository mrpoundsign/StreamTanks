const WebSocket = require('ws');
const jwt = require('jsonwebtoken');

// Generate a valid JWT for testing
const secret = Buffer.from(process.env.TWITCH_EXTENSION_SECRET || "c29tZVNlY3JldA==", 'base64');
const token = jwt.sign({
  user_id: "12345678",
  opaque_user_id: "U12345678",
  channel_id: "mrpoundsign",
  role: "broadcaster"
}, secret, { expiresIn: '1h' });

const ws = new WebSocket('wss://st-cc.poundsigndesign.com/ws/viewer?token=' + token, {
  headers: {
    'Origin': 'https://localhost',
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
  }
});

ws.on('open', function open() {
  console.log("Connected to C&C Server as Viewer!");

  // Send the initial auth payload that the server expects
  ws.send(JSON.stringify({ jwt: token }));

  // Wait 1 second and then send a mock Chat Command
  setTimeout(() => {
    const payload = {
      type: "CHAT_COMMAND",
      payload: "%fire 45 100"
    };

    console.log("Sending command:", payload);
    ws.send(JSON.stringify(payload));

    // Close the connection after sending
    setTimeout(() => {
      ws.close();
      console.log("Disconnected.");
    }, 1000);
  }, 1000);
});

ws.on('message', function message(data) {
  console.log('Received from server:', data.toString());
});

ws.on('error', console.error);
ws.on('close', () => console.log('WebSocket closed.'));
