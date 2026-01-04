// Gamelight Web Client
class GamelightClient {
    constructor() {
        this.ws = null;
        this.pc = null;
        this.dataChannels = {};
        this.participant = null;
        this.session = null;
        this.gamepadInterval = null;
        this.lastGamepadState = {};

        this.elements = {
            video: document.getElementById('video'),
            loading: document.getElementById('loading'),
            error: document.getElementById('error'),
            errorMessage: document.getElementById('error-message'),
            sidebar: document.getElementById('sidebar'),
            sidebarToggle: document.getElementById('sidebar-toggle'),
            sidebarClose: document.getElementById('sidebar-close'),
            yourRole: document.getElementById('your-role'),
            yourSlot: document.getElementById('your-slot'),
            playerActions: document.getElementById('player-actions'),
            btnJoinPlayer: document.getElementById('btn-join-player'),
            btnSpectate: document.getElementById('btn-spectate'),
            playerList: document.getElementById('player-list'),
            spectatorNum: document.getElementById('spectator-num'),
            qualitySection: document.getElementById('quality-section'),
            hostControls: document.getElementById('host-controls'),
            permissionControls: document.getElementById('permission-controls'),
            bitrate: document.getElementById('bitrate'),
            bitrateValue: document.getElementById('bitrate-value'),
            fps: document.getElementById('fps'),
            resolution: document.getElementById('resolution'),
            applyQuality: document.getElementById('apply-quality'),
        };

        this.init();
    }

    init() {
        this.setupEventListeners();
        this.connect();
    }

    setupEventListeners() {
        // Sidebar toggle
        this.elements.sidebarToggle.addEventListener('click', () => this.toggleSidebar());
        this.elements.sidebarClose.addEventListener('click', () => this.toggleSidebar(false));

        // Player actions
        this.elements.btnJoinPlayer.addEventListener('click', () => this.joinAsPlayer());
        this.elements.btnSpectate.addEventListener('click', () => this.spectate());

        // Quality controls
        this.elements.bitrate.addEventListener('input', (e) => {
            this.elements.bitrateValue.textContent = e.target.value;
        });
        this.elements.applyQuality.addEventListener('click', () => this.applyQuality());

        // Video double-click for fullscreen
        this.elements.video.addEventListener('dblclick', () => this.toggleFullscreen());

        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => this.handleKeyDown(e));
        document.addEventListener('keyup', (e) => this.handleKeyUp(e));

        // Mouse capture
        this.elements.video.addEventListener('click', () => this.capturePointer());
        document.addEventListener('pointerlockchange', () => this.onPointerLockChange());
        document.addEventListener('mousemove', (e) => this.handleMouseMove(e));
        document.addEventListener('mousedown', (e) => this.handleMouseButton(e, true));
        document.addEventListener('mouseup', (e) => this.handleMouseButton(e, false));
        document.addEventListener('wheel', (e) => this.handleMouseWheel(e));

        // Gamepad polling
        this.startGamepadPolling();
    }

    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws`;

        this.ws = new WebSocket(wsUrl);
        this.ws.onopen = () => this.onWebSocketOpen();
        this.ws.onmessage = (e) => this.onWebSocketMessage(e);
        this.ws.onclose = () => this.onWebSocketClose();
        this.ws.onerror = (e) => this.onWebSocketError(e);
    }

    onWebSocketOpen() {
        console.log('WebSocket connected');
        this.createPeerConnection();
    }

    onWebSocketMessage(event) {
        const msg = JSON.parse(event.data);

        switch (msg.type) {
            case 'session_state':
                this.handleSessionState(JSON.parse(msg.data));
                break;
            case 'answer':
                this.handleAnswer(JSON.parse(msg.data));
                break;
            case 'ice_candidate':
                this.handleICECandidate(JSON.parse(msg.data));
                break;
            case 'error':
                this.showError(msg.data);
                break;
        }
    }

    onWebSocketClose() {
        console.log('WebSocket closed');
        this.showError('Connection lost. Please refresh.');
    }

    onWebSocketError(error) {
        console.error('WebSocket error:', error);
        this.showError('Connection failed');
    }

    send(type, data) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({ type, data: JSON.stringify(data) }));
        }
    }

    async createPeerConnection() {
        const config = {
            iceServers: [
                { urls: 'stun:stun.l.google.com:19302' },
                { urls: 'stun:stun1.l.google.com:19302' },
            ]
        };

        this.pc = new RTCPeerConnection(config);

        // Handle incoming tracks
        this.pc.ontrack = (event) => {
            console.log('Received track:', event.track.kind);
            if (event.track.kind === 'video') {
                this.elements.video.srcObject = event.streams[0];
                this.elements.loading.classList.add('hidden');
            }
        };

        // Handle ICE candidates
        this.pc.onicecandidate = (event) => {
            if (event.candidate) {
                this.send('ice_candidate', {
                    candidate: event.candidate.candidate,
                    sdpMid: event.candidate.sdpMid,
                    sdpMLineIndex: event.candidate.sdpMLineIndex,
                });
            }
        };

        // Handle connection state
        this.pc.onconnectionstatechange = () => {
            console.log('Connection state:', this.pc.connectionState);
            if (this.pc.connectionState === 'failed') {
                this.showError('Connection failed');
            }
        };

        // Create data channels for input
        this.createDataChannel('mouse_relative');
        this.createDataChannel('mouse_absolute');
        this.createDataChannel('mouse_button');
        this.createDataChannel('mouse_scroll');
        this.createDataChannel('keyboard');
        this.createDataChannel('controllers');

        // Add transceivers for receiving video/audio
        this.pc.addTransceiver('video', { direction: 'recvonly' });
        this.pc.addTransceiver('audio', { direction: 'recvonly' });

        // Create offer
        const offer = await this.pc.createOffer();
        await this.pc.setLocalDescription(offer);
        this.send('offer', { sdp: offer.sdp });
    }

    createDataChannel(name) {
        const dc = this.pc.createDataChannel(name, {
            ordered: name !== 'mouse_relative', // Mouse movement can be unordered
        });
        dc.binaryType = 'arraybuffer';
        this.dataChannels[name] = dc;
    }

    async handleAnswer(answer) {
        if (this.pc) {
            await this.pc.setRemoteDescription(new RTCSessionDescription({
                type: 'answer',
                sdp: answer.sdp
            }));
        }
    }

    async handleICECandidate(candidate) {
        if (this.pc) {
            await this.pc.addIceCandidate(new RTCIceCandidate({
                candidate: candidate.candidate,
                sdpMid: candidate.sdpMid,
                sdpMLineIndex: candidate.sdpMLineIndex,
            }));
        }
    }

    handleSessionState(state) {
        this.participant = state.you;
        this.session = state.session;
        this.updateUI();
    }

    updateUI() {
        if (!this.participant) return;

        // Update your status
        const isHost = this.participant.is_host;
        const isPlayer = this.participant.role === 'player';

        this.elements.yourRole.textContent = isHost ? 'Host (Player 1)' :
            (isPlayer ? `Player ${this.participant.slot}` : 'Spectator');
        this.elements.yourRole.className = 'status-role ' +
            (isHost ? 'host' : (isPlayer ? 'player' : 'spectator'));

        this.elements.yourSlot.textContent = isPlayer ?
            `Gamepad ${this.participant.slot - 1}` :
            (isHost ? 'Keyboard + Mouse + Gamepad 0' : 'View only');

        // Show/hide player actions
        this.elements.playerActions.classList.remove('hidden');
        this.elements.btnJoinPlayer.classList.toggle('hidden', isPlayer);
        this.elements.btnSpectate.classList.toggle('hidden', !isPlayer || isHost);

        // Update player list
        if (this.session && this.session.players) {
            this.elements.playerList.innerHTML = this.session.players.map(p => `
                <li>
                    <div class="player-info">
                        <div class="player-slot slot-${p.slot}">${p.slot}</div>
                        <div>
                            <div class="player-name">${p.name || 'Player ' + p.slot}</div>
                            ${p.is_host ? '<div class="player-host">Host</div>' : ''}
                        </div>
                    </div>
                </li>
            `).join('');
        }

        // Update spectator count
        this.elements.spectatorNum.textContent = this.session?.spectators || 0;

        // Show host controls
        this.elements.qualitySection.classList.toggle('hidden', !isHost);
        this.elements.hostControls.classList.toggle('hidden', !isHost);

        if (isHost) {
            this.updatePermissionControls();
        }
    }

    updatePermissionControls() {
        if (!this.session || !this.session.players) return;

        const otherPlayers = this.session.players.filter(p => !p.is_host);
        this.elements.permissionControls.innerHTML = otherPlayers.map(p => `
            <div class="permission-item">
                <span>Player ${p.slot}</span>
                <div>
                    <label>
                        KB
                        <div class="toggle ${p.can_keyboard ? 'active' : ''}"
                             data-player="${p.id}" data-type="keyboard"></div>
                    </label>
                    <label>
                        Mouse
                        <div class="toggle ${p.can_mouse ? 'active' : ''}"
                             data-player="${p.id}" data-type="mouse"></div>
                    </label>
                </div>
            </div>
        `).join('');

        // Add click handlers for toggles
        this.elements.permissionControls.querySelectorAll('.toggle').forEach(toggle => {
            toggle.addEventListener('click', () => {
                const playerId = toggle.dataset.player;
                const type = toggle.dataset.type;
                const active = !toggle.classList.contains('active');

                this.send('set_permission', {
                    target_id: playerId,
                    keyboard: type === 'keyboard' ? active : false,
                    mouse: type === 'mouse' ? active : false,
                });
            });
        });
    }

    toggleSidebar(show) {
        if (typeof show === 'boolean') {
            this.elements.sidebar.classList.toggle('collapsed', !show);
        } else {
            this.elements.sidebar.classList.toggle('collapsed');
        }
    }

    toggleFullscreen() {
        if (document.fullscreenElement) {
            document.exitFullscreen();
        } else {
            document.documentElement.requestFullscreen();
        }
    }

    capturePointer() {
        if (this.canUseInput()) {
            this.elements.video.requestPointerLock();
        }
    }

    onPointerLockChange() {
        const locked = document.pointerLockElement === this.elements.video;
        console.log('Pointer lock:', locked);
    }

    canUseInput() {
        return this.participant &&
               (this.participant.role === 'player' || this.participant.is_host);
    }

    canUseKeyboard() {
        return this.participant && this.participant.can_keyboard;
    }

    canUseMouse() {
        return this.participant && this.participant.can_mouse;
    }

    // Input handlers
    handleKeyDown(e) {
        if (!this.canUseKeyboard()) return;
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT') return;

        // F11 for fullscreen
        if (e.key === 'F11') {
            e.preventDefault();
            this.toggleFullscreen();
            return;
        }

        // Escape to release pointer lock
        if (e.key === 'Escape') {
            return;
        }

        e.preventDefault();
        this.sendKeyboard(e.keyCode, 0x03, this.getModifiers(e));
    }

    handleKeyUp(e) {
        if (!this.canUseKeyboard()) return;
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT') return;

        e.preventDefault();
        this.sendKeyboard(e.keyCode, 0x04, this.getModifiers(e));
    }

    getModifiers(e) {
        let mods = 0;
        if (e.shiftKey) mods |= 0x01;
        if (e.ctrlKey) mods |= 0x02;
        if (e.altKey) mods |= 0x04;
        return mods;
    }

    handleMouseMove(e) {
        if (!this.canUseMouse()) return;
        if (document.pointerLockElement !== this.elements.video) return;

        const dx = e.movementX;
        const dy = e.movementY;

        if (dx !== 0 || dy !== 0) {
            this.sendMouseMove(dx, dy);
        }
    }

    handleMouseButton(e, down) {
        if (!this.canUseMouse()) return;
        if (document.pointerLockElement !== this.elements.video) return;

        e.preventDefault();

        let button = 1;
        if (e.button === 1) button = 2;
        if (e.button === 2) button = 3;

        this.sendMouseButton(button, down ? 0x07 : 0x08);
    }

    handleMouseWheel(e) {
        if (!this.canUseMouse()) return;
        if (document.pointerLockElement !== this.elements.video) return;

        e.preventDefault();
        const amount = Math.sign(e.deltaY) * -120; // Standard scroll units
        this.sendMouseScroll(amount);
    }

    // Data channel sends
    sendMouseMove(dx, dy) {
        const dc = this.dataChannels['mouse_relative'];
        if (dc && dc.readyState === 'open') {
            const data = new Int16Array([dx, dy]);
            dc.send(data.buffer);
        }
    }

    sendMouseButton(button, action) {
        const dc = this.dataChannels['mouse_button'];
        if (dc && dc.readyState === 'open') {
            const data = new Uint8Array([button, action]);
            dc.send(data.buffer);
        }
    }

    sendMouseScroll(amount) {
        const dc = this.dataChannels['mouse_scroll'];
        if (dc && dc.readyState === 'open') {
            const data = new Int16Array([amount]);
            dc.send(data.buffer);
        }
    }

    sendKeyboard(keyCode, action, modifiers) {
        const dc = this.dataChannels['keyboard'];
        if (dc && dc.readyState === 'open') {
            const data = new ArrayBuffer(4);
            const view = new DataView(data);
            view.setUint16(0, keyCode, true);
            view.setUint8(2, action);
            view.setUint8(3, modifiers);
            dc.send(data);
        }
    }

    sendController(controllerNum, buttons, lt, rt, lsx, lsy, rsx, rsy) {
        const dc = this.dataChannels['controllers'];
        if (dc && dc.readyState === 'open') {
            const data = new ArrayBuffer(15);
            const view = new DataView(data);
            view.setUint8(0, controllerNum);
            view.setUint32(1, buttons, true);
            view.setUint8(5, lt);
            view.setUint8(6, rt);
            view.setInt16(7, lsx, true);
            view.setInt16(9, lsy, true);
            view.setInt16(11, rsx, true);
            view.setInt16(13, rsy, true);
            dc.send(data);
        }
    }

    // Gamepad polling
    startGamepadPolling() {
        this.gamepadInterval = setInterval(() => this.pollGamepads(), 16); // ~60fps
    }

    pollGamepads() {
        if (!this.canUseInput()) return;

        const gamepads = navigator.getGamepads();
        for (let i = 0; i < gamepads.length; i++) {
            const gp = gamepads[i];
            if (!gp) continue;

            const state = this.getGamepadState(gp);
            const lastState = this.lastGamepadState[i];

            // Only send if changed
            if (!lastState || !this.gamepadStatesEqual(state, lastState)) {
                this.sendController(
                    i,
                    state.buttons,
                    state.lt,
                    state.rt,
                    state.lsx,
                    state.lsy,
                    state.rsx,
                    state.rsy
                );
                this.lastGamepadState[i] = state;
            }
        }
    }

    getGamepadState(gp) {
        // Map standard gamepad buttons to Xbox layout
        let buttons = 0;

        // Face buttons (indices may vary by controller)
        if (gp.buttons[0]?.pressed) buttons |= 0x1000; // A
        if (gp.buttons[1]?.pressed) buttons |= 0x2000; // B
        if (gp.buttons[2]?.pressed) buttons |= 0x4000; // X
        if (gp.buttons[3]?.pressed) buttons |= 0x8000; // Y

        // Bumpers
        if (gp.buttons[4]?.pressed) buttons |= 0x0100; // LB
        if (gp.buttons[5]?.pressed) buttons |= 0x0200; // RB

        // Start/Back
        if (gp.buttons[8]?.pressed) buttons |= 0x0020; // Back
        if (gp.buttons[9]?.pressed) buttons |= 0x0010; // Start

        // Stick buttons
        if (gp.buttons[10]?.pressed) buttons |= 0x0040; // LS
        if (gp.buttons[11]?.pressed) buttons |= 0x0080; // RS

        // D-pad
        if (gp.buttons[12]?.pressed) buttons |= 0x0001; // Up
        if (gp.buttons[13]?.pressed) buttons |= 0x0002; // Down
        if (gp.buttons[14]?.pressed) buttons |= 0x0004; // Left
        if (gp.buttons[15]?.pressed) buttons |= 0x0008; // Right

        // Guide button
        if (gp.buttons[16]?.pressed) buttons |= 0x0400; // Guide

        // Triggers (analog 0-255)
        const lt = Math.round((gp.buttons[6]?.value || 0) * 255);
        const rt = Math.round((gp.buttons[7]?.value || 0) * 255);

        // Sticks (-32768 to 32767)
        const deadzone = 0.15;
        const applyDeadzone = (v) => Math.abs(v) < deadzone ? 0 : v;

        const lsx = Math.round(applyDeadzone(gp.axes[0] || 0) * 32767);
        const lsy = Math.round(applyDeadzone(-(gp.axes[1] || 0)) * 32767); // Invert Y
        const rsx = Math.round(applyDeadzone(gp.axes[2] || 0) * 32767);
        const rsy = Math.round(applyDeadzone(-(gp.axes[3] || 0)) * 32767); // Invert Y

        return { buttons, lt, rt, lsx, lsy, rsx, rsy };
    }

    gamepadStatesEqual(a, b) {
        return a.buttons === b.buttons &&
               a.lt === b.lt && a.rt === b.rt &&
               a.lsx === b.lsx && a.lsy === b.lsy &&
               a.rsx === b.rsx && a.rsy === b.rsy;
    }

    // Actions
    joinAsPlayer() {
        this.send('join_as_player', {});
    }

    spectate() {
        this.send('spectate', {});
    }

    applyQuality() {
        const [width, height] = this.elements.resolution.value.split('x').map(Number);
        this.send('set_quality', {
            bitrate: parseInt(this.elements.bitrate.value),
            fps: parseInt(this.elements.fps.value),
            width,
            height,
        });
    }

    showError(message) {
        this.elements.loading.classList.add('hidden');
        this.elements.error.classList.remove('hidden');
        this.elements.errorMessage.textContent = message;
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    window.gamelight = new GamelightClient();
});
