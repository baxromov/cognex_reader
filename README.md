# WebSocket-Telnet Bridge

A WebSocket server that bridges WebSocket connections to Telnet servers, allowing multiple clients to share the same Telnet session. This application ensures seamless real-time communication between WebSocket clients and Telnet servers, providing robust and flexible features for collaborative or monitoring use cases.

---

## Features

- **Multiple Channel Support**:  
  Maintain and manage multiple communication channels between clients and Telnet servers.

- **Automatic Reconnection**:  
  If the Telnet server becomes unavailable, the application automatically retries to reconnect, ensuring minimal disruption.

- **Shared Telnet Sessions**:  
  Multiple WebSocket clients can connect to the same Telnet session collaboratively, sharing input and output in real time.

- **Real-Time Broadcasting**:  
  Messages from the Telnet server are broadcasted to all connected WebSocket clients in real time, ensuring instant updates.

- **Connection Status Monitoring**:  
  Provides feedback on the connection health and events, such as disconnections, successes, or errors.

---

## WebSocket Usage

### Use Cases
1. **Collaborative Terminal Access**:  
   Useful for scenarios where teams need to troubleshoot or monitor server logs together.

2. **Telnet to WebSocket Conversion**:  
   Allows modern front-end clients (like web browsers or custom apps) to interact with legacy Telnet servers.

3. **Real-Time Data Visualization**:  
   Telnet server outputs can be parsed, processed, and displayed dynamically on user interfaces.

---

### Error Scenarios and Handling
WebSocket communication in the context of this project may return errors under the following conditions:

1. **Telnet Server Unreachable**:
    - **Cause**: The target Telnet server is down or unreachable.
    - **Response**: An error message is sent back to all connected WebSocket clients with details.
    - **Solution**: Ensure the Telnet server is active and reachable from the machine hosting the WebSocket server.

2. **Connection Drops**:
    - **Cause**: Interruption in the Telnet server communication or WebSocket client disconnection.
    - **Response**: Inform all connected WebSocket clients of the session's status.
    - **Solution**: The application attempts to automatically reconnect to the Telnet server.

3. **Invalid Client Messages**:
    - **Cause**: Clients send improperly formatted WebSocket messages.
    - **Response**: Reject the message and respond with an error description.
    - **Solution**: Clients must follow the protocol when sending actions or data.

4. **Session Overlap Issues**:
    - **Cause**: Conflicts in shared Telnet sessions due to one client overwriting input/output from another.
    - **Response**: Implement controlled session sharing mechanisms with prioritization.
    - **Solution**: Use clear access rules and synchronization logic in the implementation.

---

### Why Use WebSocket-Telnet Bridging?

1. **Combining Modern Interfaces with Legacy Systems**:  
   Telnet remains a common protocol in legacy systems. By bridging it with WebSockets, you can modernize and extend these systems for use in web-based environments.

2. **Shared Collaborations**:  
   This project facilitates collaborative usage of Telnet sessions, allowing multiple users to work on the same session seamlessly.

3. **Real-Time Monitoring**:  
   Provides real-time feedback from Telnet servers to connected WebSocket clients, making it incredibly useful for interactive dashboards, live monitoring, or debugging tasks.

4. **Persistent Connections**:  
   WebSockets and Telnet both support long-lived connections, enabling efficient and continuous communication with minimal overhead.

---

## Example Usage

Here’s an example of how to connect and use the WebSocket-Telnet Bridge with **JavaScript** and **curl**.

### Using JavaScript WebSocket Client
This script demonstrates how to establish a WebSocket connection and exchange data with the WebSocket-Telnet Bridge.
```javascript
// Connect to the WebSocket server with channel name and telnet host
const socket = new WebSocket('ws://0.0.0.0:5001/mychannel?host=telnet.example.com:23');

// Event: Connection opened
socket.onopen = () => {
console.log('Connected to telnet channel');

    // Example: Send command to the Telnet server
    socket.send('ls -al');

};

// Event: Receive message from the server
socket.onmessage = (event) => {
console.log('Telnet output:', event.data);
};

// Event: Handle errors
socket.onerror = (error) => {
console.error('WebSocket error:', error);
};

// Event: Connection closed
socket.onclose = (event) => {
console.log('Channel connection closed:', event.reason);
};

```
---

### Using `curl` Command
To test the WebSocket-Telnet Bridge using `curl`, you can establish a WebSocket connection as follows:

```shell script
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Host: example.com" \
-H "Origin: http://0.0.0.0:5001" \
--http1.1 ws://0.0.0.0:5001
```


This creates a WebSocket connection where you can observe messages being sent by the server.

---

## Project Setup

### Prerequisites
- Ensure the WebSocket server is set up and configured with the correct Telnet server information.
- Confirm that the Telnet server is accessible from the WebSocket server's hosting machine.

---

## Conclusion

The WebSocket-Telnet Bridge offers a modern, efficient solution for interfacing with legacy Telnet systems. By bridging these two protocols, the project enables collaborative, real-time, and low-overhead interaction for shared Telnet sessions. The system is easy to set up and operate, making it adaptable for use cases such as server monitoring, collaborative debugging, and real-time interaction with legacy systems.