export namespace main {
	
	export class ChatItem {
	    id: number;
	    kind: string;
	    text?: string;
	    name?: string;
	    summary?: string;
	    escalation?: notes.Escalation;
	    at: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.text = source["text"];
	        this.name = source["name"];
	        this.summary = source["summary"];
	        this.escalation = this.convertValues(source["escalation"], notes.Escalation);
	        this.at = source["at"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LimitWindow {
	    utilization: number;
	    resets_at: number;
	
	    static createFrom(source: any = {}) {
	        return new LimitWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.utilization = source["utilization"];
	        this.resets_at = source["resets_at"];
	    }
	}
	export class RateLimit {
	    five_hour?: LimitWindow;
	    seven_day?: LimitWindow;
	
	    static createFrom(source: any = {}) {
	        return new RateLimit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.five_hour = this.convertValues(source["five_hour"], LimitWindow);
	        this.seven_day = this.convertValues(source["seven_day"], LimitWindow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SessionSnapshot {
	    session: notes.Session;
	    agents: notes.Agent[];
	    tasks: notes.Task[];
	    notes: notes.Note[];
	    chat: ChatItem[];
	    escalations: notes.Escalation[];
	
	    static createFrom(source: any = {}) {
	        return new SessionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session = this.convertValues(source["session"], notes.Session);
	        this.agents = this.convertValues(source["agents"], notes.Agent);
	        this.tasks = this.convertValues(source["tasks"], notes.Task);
	        this.notes = this.convertValues(source["notes"], notes.Note);
	        this.chat = this.convertValues(source["chat"], ChatItem);
	        this.escalations = this.convertValues(source["escalations"], notes.Escalation);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace notes {
	
	export class Agent {
	    id: number;
	    session_id: number;
	    parent_id?: number;
	    role: string;
	    task_id?: number;
	    status: string;
	    pid?: number;
	    log_path?: string;
	    exit_code?: number;
	    created_at: string;
	    exited_at?: string;
	    context_used?: number;
	    context_window?: number;
	
	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.parent_id = source["parent_id"];
	        this.role = source["role"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.pid = source["pid"];
	        this.log_path = source["log_path"];
	        this.exit_code = source["exit_code"];
	        this.created_at = source["created_at"];
	        this.exited_at = source["exited_at"];
	        this.context_used = source["context_used"];
	        this.context_window = source["context_window"];
	    }
	}
	export class AgentEvent {
	    id: number;
	    agent_id: number;
	    event_type: string;
	    payload: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.agent_id = source["agent_id"];
	        this.event_type = source["event_type"];
	        this.payload = source["payload"];
	        this.created_at = source["created_at"];
	    }
	}
	export class Escalation {
	    id: number;
	    session_id: number;
	    agent_id: number;
	    question: string;
	    context: string;
	    status: string;
	    answer?: string;
	
	    static createFrom(source: any = {}) {
	        return new Escalation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.agent_id = source["agent_id"];
	        this.question = source["question"];
	        this.context = source["context"];
	        this.status = source["status"];
	        this.answer = source["answer"];
	    }
	}
	export class Note {
	    id: number;
	    board_id: number;
	    author_agent_id: number;
	    type: string;
	    content: string;
	    status: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Note(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.board_id = source["board_id"];
	        this.author_agent_id = source["author_agent_id"];
	        this.type = source["type"];
	        this.content = source["content"];
	        this.status = source["status"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class Session {
	    id: number;
	    title: string;
	    status: string;
	    work_dir: string;
	    created_at: string;
	    agent_count: number;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.work_dir = source["work_dir"];
	        this.created_at = source["created_at"];
	        this.agent_count = source["agent_count"];
	    }
	}
	export class Task {
	    id: number;
	    session_id: number;
	    title: string;
	    description: string;
	    status: string;
	    agent_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.agent_id = source["agent_id"];
	    }
	}

}

