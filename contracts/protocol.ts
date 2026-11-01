/** Stable v1 transport contract. Runtime JS client can use JSDoc import types.
 * Phase-2 snapshot/chunk/agent payloads added.
 * Phase-4 AI agent talk & reply with citations added.
 * Cooperative Survival Scenario: GameState, Action and Investigation payloads.
 */
export const PROTOCOL_VERSION = 1 as const;
export interface User { id: string; username: string }
export interface Position { x: number; y: number; z: number }
export interface PlayerState {
  player_id: string;
  position: Position;
  yaw: number;
  last_seq: number;
  updated_at: string; // RFC3339 UTC
}
export interface Capabilities { movement: boolean; world: boolean; agents: boolean }
export interface PlayerInput { seq: number; x: number; y: number; z: number; yaw: number }
export interface ErrorPayload { code: string; message: string }

export interface PlayerSnapshotItem {
  id: string;
  name: string;
  x: number;
  y: number;
  z: number;
  yaw: number;
}

export interface WorldSnapshot {
  server_time: number;
  players: PlayerSnapshotItem[];
}

export interface Citation {
	source_url?: string;
	document_id?: string;
	content_mode?: 'summary' | 'translated_summary' | 'public_domain_excerpt' | 'game_rules';
	relation_to_person?: 'person' | 'mission_context' | 'role_background';
  source_title: string;
  section: string;
  excerpt: string;
  confidence: string;
}

export interface AgentTalkRequest {
  agent_id: string;
  text: string;
  dialogue_mode?: 'articles' | 'casual';
}

export interface AgentReply {
  dialogue_mode?: 'articles' | 'casual';
  retrieval?: {mode:'hybrid' | 'bm25' | 'bm25_fallback';embedding_model?:string;degraded_reason?:string};
  status?: string;
  grounding?: string;
  model?: string;
  fallback_reason?: string;
 tool_transport?: string;
  agent_id: string;
  agent_name: string;
  text: string;
  citation?: Citation;
  citations?: Citation[];
  mode?: 'model' | 'knowledge_base' | 'rules' | 'no_sources' | 'unavailable';
  evidence?: RAGChunk[];
  analysis?: Record<string, unknown>;
}

export interface RAGChunk {
  id: string;
  text: string;
  agents: string[];
  topics?: string[];
  requires_facts?: string[];
  source: {
    id: string;
    title: string;
    url?: string;
    locator?: string;
    content_mode?: string;
    relation_to_person?: string;
    language?: string;
  };
}

export interface GameState {
 version:number;
  cycle: number;
  max_cycles: number;
  oxygen: number;
  energy: number;
  hull: number;
  reactor_active: boolean;
  cooling_repaired: boolean;
  greenhouse_active: boolean;
  battery_available: boolean;
  course_calculated: boolean;
  archive_restored: boolean;
  investigation_solved: boolean;
  game_over: boolean;
  victory: boolean;
  victory_type: string;
  unlocked_clues: string[];
  logs: string[];
}

export interface GameActionPayload {
 expected_version?:number;
  action: string;
}

export interface InvestigationSolvePayload {
 expected_version?:number;
  clues: string[];
}

export interface ClientPayloads {
  'player.init': { client_time?: number; username?: string };
  'player.input': PlayerInput;
  'agent.talk': AgentTalkRequest;
  'game.action': GameActionPayload;
  'game.preview': {action:string;expected_version:number};
  'investigation.solve': InvestigationSolvePayload;
  'room.get': Record<string, never>;
  'room.create': {name?:string};
  'room.join': {code:string};
  'room.leave': Record<string,never>;
  'room.role': {role:'captain'|'engineer'|'biologist'|'navigator'|'commander'};
  'room.ready': {ready:boolean};
  'room.start': Record<string,never>;
  'room.reset': Record<string,never>;
  'chat.send': {text:string};
  'decision.vote': {approve:boolean;expected_version:number};
  'dialogue.history': {agent_id:string;dialogue_mode?:'articles'|'casual'};
  'player.teleport': {target:string};
  'experiment.start': Record<string,never>;
  'experiment.preview': {speed:number};
  'experiment.command': {action:'join'|'predict'|'speed'|'run';expected_version:number;role?:'observer'|'operator';prediction?:'same'|'different';speed?:number};
  ping: Record<string, unknown>;
}
export interface ServerPayloads {
  'room.state': RoomState;
  'player.position': PlayerState;
  'dialogue.history': {dialogue_mode?:'articles'|'casual';messages:Array<{role:string;text:string;dialogue_mode?:'articles'|'casual';reply?:AgentReply}>};
  'chat.sent': {name:string;text:string;at:number};
  'decision.pending': {pending:true};
  'experiment.started': ExperimentState;
  'experiment.accepted': {ok:boolean};
  'experiment.preview': {gamma:number;speed:number;cooperation_credit:false};
  'player.ready': { user: User; state: PlayerState; capabilities: Capabilities };
  'world.snapshot': WorldSnapshot;
  'agent.reply': AgentReply;
  'game.state': GameState;
  'game.preview': {action:string;allowed:boolean;before:GameState;after:GameState;reason?:string;read_only:true};
  'investigation.result': { solved: boolean; message: string };
  pong: Record<string, unknown>;
  error: ErrorPayload;
}
export interface Envelope<T extends string, P> {
  protocol_version: typeof PROTOCOL_VERSION;
  type: T;
  request_id: string; // nonempty for requests/replies; empty for unsolicited events
  payload: P;
}
export type ClientMessage = { [K in keyof ClientPayloads]: Envelope<K, ClientPayloads[K]> }[keyof ClientPayloads];
export type ServerMessage = { [K in keyof ServerPayloads]: Envelope<K, ServerPayloads[K]> }[keyof ServerPayloads];
export interface Credentials { username: string; password: string }
export interface AuthResponse { success: true; user: User }

export interface RoomState{code:string;name?:string;owner?:string;started?:boolean;generation?:number;members?:Array<{id:string;name:string;role:string;ready:boolean;online:boolean}>;proposal?:{action:string;version:number;voters:string[];approved:Record<string,boolean>};experiment?:ExperimentState;chat?:Array<{name:string;text:string;at:number}>}
export interface ExperimentState{id:string;type:'light_clock';version:number;speed:number;members:Record<string,string>;predictions:Record<string,string>;ran:boolean;gamma:number;started_at:number}
