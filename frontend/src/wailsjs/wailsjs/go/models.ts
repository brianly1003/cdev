export namespace main {
	
	export class ConnectionStatus {
	    server_running: boolean;
	    server_port: number;
	    server_address: string;
	    connected_clients: number;
	    claude_state: string;
	    active_repo: string;
	    session_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server_running = source["server_running"];
	        this.server_port = source["server_port"];
	        this.server_address = source["server_address"];
	        this.connected_clients = source["connected_clients"];
	        this.claude_state = source["claude_state"];
	        this.active_repo = source["active_repo"];
	        this.session_id = source["session_id"];
	    }
	}
	export class Repository {
	    id: string;
	    path: string;
	    display_name: string;
	    git_branch?: string;
	    git_remote?: string;
	    is_active: boolean;
	    last_active?: string;
	
	    static createFrom(source: any = {}) {
	        return new Repository(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.display_name = source["display_name"];
	        this.git_branch = source["git_branch"];
	        this.git_remote = source["git_remote"];
	        this.is_active = source["is_active"];
	        this.last_active = source["last_active"];
	    }
	}

}

