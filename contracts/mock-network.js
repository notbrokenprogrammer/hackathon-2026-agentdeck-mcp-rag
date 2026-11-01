/** Explicit frontend-only mock. Import it instead of NetworkManager in a local
 * development entry point. It never activates implicitly when real auth fails.
 * Implements the current client's register/login/connectWebSocket/on/off/send API.
 */
export class MockNetworkManager {
    constructor() {
        this.listeners = new Map();
        this.currentUser = null;
        this.isConnected = false;
        this.requestIdCounter = 1;
        this.capabilities = { movement: false, world: false, agents: false };
    }
    async register(username) { return { success: true, user: { id: 'mock-player', username } }; }
    async login(username) {
        this.currentUser = { id: 'mock-player', username };
        return { success: true, user: this.currentUser };
    }
    connectWebSocket() {
        if (!this.currentUser) throw new Error('Call mock login first');
        if (this.isConnected) return;
        this.isConnected = true;
        queueMicrotask(() => {
            this._emit('connect', { user: this.currentUser });
            this.send('player.init');
        });
    }
    send(type, payload = {}) {
        if (!this.isConnected) return null;
        if (type === 'player.input' && !this.capabilities.movement) return null;
        const request_id = `req-${this.requestIdCounter++}`;
        let response;
        if (type === 'player.init') response = {
            type: 'player.ready', payload: {
                user: this.currentUser, capabilities: this.capabilities,
                state: { player_id: this.currentUser.id, position: { x: 0, y: 1.7, z: 5 }, yaw: 0, last_seq: 0, updated_at: new Date().toISOString() }
            }
        };
        else if (type === 'ping') response = { type: 'pong', payload };
        else response = { type: 'error', payload: { code: 'unknown_type', message: 'Unknown message type' } };
        const message = { protocol_version: 1, request_id, ...response };
        queueMicrotask(() => { this._emit('message', message); this._emit(message.type, message.payload, message); });
        return request_id;
    }
    on(event, callback) {
        if (!this.listeners.has(event)) this.listeners.set(event, new Set());
        this.listeners.get(event).add(callback);
    }
    off(event, callback) { this.listeners.get(event)?.delete(callback); }
    _emit(event, ...args) { this.listeners.get(event)?.forEach(callback => callback(...args)); }
    disconnect() { this.isConnected = false; this._emit('disconnect', { code: 1000 }); }
}
