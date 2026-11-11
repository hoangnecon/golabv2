package scripts

import (
	"encoding/json"
	"fmt"
)

// ScriptExecPython evaluates Python silently via Jupyter WebSocket /api/kernels/<id>/channels
// on the primary kernel when idle, or spawns/reuses an out-of-band sidecar kernel via POST /api/kernels
// when the primary kernel is busy and preferSidecarWhenBusy is true.
func ScriptExecPython(code string, preferSidecarWhenBusy bool) string {
	payload, _ := json.Marshal(map[string]any{
		"code":           code,
		"prefer_sidecar": preferSidecarWhenBusy,
	})
	return fmt.Sprintf(`(async () => {
		// GOLAB_OP:exec_python %s
		const cfg = %s;
		const stripXSSI = (text) => {
			let s = String(text || "");
			if (s.startsWith(")]}'\n")) s = s.substring(5);
			else if (s.startsWith(")]}'")) s = s.substring(4);
			return s.trim();
		};
		const withTimeout = (p, ms, label) => Promise.race([
			p,
			new Promise((_, reject) => setTimeout(() => reject(new Error(label + " timed out")), ms))
		]);

		const nb = window.colab?.global?.notebook;
		const kernel = nb?.getKernel ? nb.getKernel() : null;
		if (!kernel) {
			return JSON.stringify({ error: "No active Colab kernel connected." });
		}

		const kState = String(kernel.getState ? kernel.getState() : (kernel.state || "")).toLowerCase();
		const isBusy = !!(nb?.isExecuting ? nb.isExecuting() : false) || kState.includes("busy");
		const useSidecar = isBusy && !!cfg.prefer_sidecar;

		const xhr = (kernel.getKernelXhr ? kernel.getKernelXhr() : null) ||
			kernel.xhr ||
			(kernel.connection?.getKernelXhr ? kernel.connection.getKernelXhr() : null) ||
			kernel.connection?.xhr ||
			null;
		const conn = kernel.connection || null;

		// Resolve target kernel ID: primary kernel when idle, or sidecar kernel when busy
		let targetKernelId = "";
		if (!useSidecar) {
			targetKernelId = String(
				(conn?.getKernelId ? conn.getKernelId() : "") ||
				conn?.kernelId ||
				kernel.id ||
				""
			);
			if (!targetKernelId && xhr) {
				try {
					const kResp = await withTimeout(xhr.fetch("/api/kernels", { method: "GET" }), 4000, "list kernels");
					if (kResp?.ok) {
						const kList = JSON.parse(stripXSSI(await kResp.text()));
						if (Array.isArray(kList) && kList.length > 0 && kList[0]?.id) {
							targetKernelId = String(kList[0].id);
						}
					}
				} catch (_) {}
			}
		} else {
			if (!xhr) {
				return JSON.stringify({ error: "Kernel XHR tunnel unavailable for sidecar execution." });
			}
			let sidecarId = window.__golabSidecarKernelId || "";
			if (sidecarId) {
				try {
					const checkResp = await withTimeout(xhr.fetch("/api/kernels/" + sidecarId, { method: "GET" }), 4000, "check sidecar");
					if (!checkResp.ok) sidecarId = "";
				} catch (_) {
					sidecarId = "";
				}
			}
			if (!sidecarId) {
				const createResp = await withTimeout(xhr.fetch("/api/kernels", {
					method: "POST",
					headers: new Headers({ "Content-Type": "application/json" }),
					body: JSON.stringify({ name: "python3" })
				}), 8000, "spawn sidecar kernel");
				if (!createResp.ok) {
					return JSON.stringify({ error: "Failed to spawn sidecar kernel: HTTP " + createResp.status });
				}
				const created = JSON.parse(stripXSSI(await createResp.text()));
				sidecarId = String(created.id || "");
				window.__golabSidecarKernelId = sidecarId;
			}
			targetKernelId = sidecarId;
		}

		// Primary path: Direct Jupyter /api/kernels/<id>/channels WebSocket over Colab runtime proxy
		const ep = kernel.getEndpoint ? kernel.getEndpoint() : (kernel.endpoint || null);
		let proxyInfo = null;
		try {
			proxyInfo = ep?.getRuntimeProxyInfo ? await Promise.resolve(ep.getRuntimeProxyInfo()) : ep?.runtimeProxyInfo;
		} catch (_) {}
		const baseURL = String(proxyInfo?.url || "").replace(/^http/, "ws").replace(/\/$/, "");
		const token = String(proxyInfo?.token || "");

		if (baseURL && targetKernelId) {
			const msgId = "golab_" + Math.random().toString(36).slice(2) + "_" + Date.now();
			const sessionId = "golab_sess_" + targetKernelId;
			const wsURL = baseURL + "/api/kernels/" + encodeURIComponent(targetKernelId) + "/channels?session_id=" + encodeURIComponent(sessionId) +
				(token ? "&colab-runtime-proxy-token=" + encodeURIComponent(token) : "");

			try {
				const execViaWS = await withTimeout(new Promise((resolve, reject) => {
					let stdout = "";
					let stderr = "";
					let resultText = "";
					let errMsg = "";
					let settled = false;
					const ws = new WebSocket(wsURL);
					ws.onopen = () => {
						ws.send(JSON.stringify({
							header: {
								msg_id: msgId,
								username: "golab",
								session: sessionId,
								msg_type: "execute_request",
								version: "5.3"
							},
							parent_header: {},
							metadata: {},
							content: {
								code: cfg.code,
								silent: false,
								store_history: false,
								user_expressions: {},
								allow_stdin: false,
								stop_on_error: true
							},
							buffers: [],
							channel: "shell"
						}));
					};
					ws.onerror = () => {
						if (!settled) {
							settled = true;
							reject(new Error("Kernel WebSocket error"));
						}
					};
					ws.onclose = () => {
						if (!settled) {
							settled = true;
							reject(new Error("Kernel WebSocket closed before execute_reply"));
						}
					};
					ws.onmessage = (ev) => {
						try {
							const msg = JSON.parse(String(ev.data || "{}"));
							if (msg?.parent_header?.msg_id !== msgId) return;
							const mt = msg?.header?.msg_type || msg?.msg_type || "";
							const c = msg?.content || {};
							if (mt === "stream") {
								if (c.name === "stderr") stderr += String(c.text || "");
								else stdout += String(c.text || "");
							} else if (mt === "execute_result" || mt === "display_data") {
								const plain = c?.data?.["text/plain"];
								if (plain) resultText += String(plain);
							} else if (mt === "error") {
								errMsg = (c.ename || "Error") + ": " + (c.evalue || "");
							} else if (mt === "execute_reply") {
								settled = true;
								try { ws.close(); } catch (_) {}
								resolve({ stdout, stderr, result: resultText, error: errMsg });
							}
						} catch (_) {}
					};
				}), 30000, "kernel channels execute_request");

				return JSON.stringify({
					used_sidecar: useSidecar,
					kernel_id: targetKernelId,
					stdout: execViaWS.stdout,
					stderr: execViaWS.stderr,
					result: execViaWS.result,
					error: execViaWS.error
				});
			} catch (wsErr) {
				if (useSidecar) {
					return JSON.stringify({ error: wsErr.toString() });
				}
			}
		}

		// Fallback path for primary kernel (or lightweight mocks): kernel.connection.execute / kernel.execute
		const execTarget = (conn && typeof conn.execute === "function") ? conn : (typeof kernel.execute === "function" ? kernel : null);
		if (!execTarget) {
			return JSON.stringify({ error: "No executable Colab kernel channel available." });
		}

		let stdout = "";
		let stderr = "";
		let errMsg = "";
		let resultText = "";
		try {
			const ioHandler = {
				supportsUserInput: () => false,
				output: (msg) => {
					const mt = msg?.header?.msg_type || msg?.msg_type || "";
					const content = msg?.content || {};
					if (mt === "stream") {
						if (content.name === "stderr") stderr += String(content.text || "");
						else stdout += String(content.text || "");
					} else if (mt === "execute_result" || mt === "display_data") {
						const plain = content?.data?.["text/plain"];
						if (plain) resultText += String(plain);
					} else if (mt === "error") {
						errMsg = (content.ename || "Error") + ": " + (content.evalue || "");
					}
				}
			};
			const res = await withTimeout(
				Promise.resolve(execTarget.execute(cfg.code, ioHandler, { silent: false, storeHistory: false })),
				30000,
				"kernel.execute"
			);
			if (!stdout && typeof res === "object" && res !== null) {
				if (res.stdout) stdout = String(res.stdout);
				if (res.result && !resultText) resultText = String(res.result);
			}
		} catch (err) {
			errMsg = err.toString();
		}
		return JSON.stringify({
			used_sidecar: false,
			kernel_id: targetKernelId || String(kernel.id || ""),
			stdout,
			stderr,
			result: resultText,
			error: errMsg
		});
	})()`, string(payload), string(payload))
}

// PythonBackgroundJobScript generates Python code to start, poll, or kill an out-of-band job in /tmp/.golab_jobs/.
func PythonBackgroundJobScript(action, jobID, command, cwd string, tailLines int) string {
	paramsJSON, _ := json.Marshal(map[string]any{
		"golab_job_action": action,
		"job_id":           jobID,
		"command":          command,
		"cwd":              cwd,
		"tail_lines":       tailLines,
	})
	return fmt.Sprintf(`import json, os, signal, subprocess, time, uuid
_cfg = json.loads(%q)
_base = "/tmp/.golab_jobs"
os.makedirs(_base, exist_ok=True)
_act = _cfg["golab_job_action"]
_jid = _cfg["job_id"] or ("job_" + uuid.uuid4().hex[:10])
_meta_path = os.path.join(_base, _jid + ".json")
_log_path = os.path.join(_base, _jid + ".log")
_exit_path = os.path.join(_base, _jid + ".exit")

def _pid_alive(pid):
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except Exception:
        return False

def _read_status():
    if not os.path.exists(_meta_path):
        raise RuntimeError(f"Background job not found: {_jid}")
    with open(_meta_path, "r", encoding="utf-8") as f:
        meta = json.load(f)
    pid = int(meta.get("pid", 0))
    exit_code = None
    if os.path.exists(_exit_path):
        try:
            with open(_exit_path, "r", encoding="utf-8") as ef:
                exit_code = int(ef.read().strip())
        except Exception:
            exit_code = None
    running = (exit_code is None) and _pid_alive(pid)
    log_bytes = os.path.getsize(_log_path) if os.path.exists(_log_path) else 0
    tail = ""
    if os.path.exists(_log_path):
        with open(_log_path, "r", encoding="utf-8", errors="replace") as lf:
            lines = lf.readlines()
            n = max(1, int(_cfg.get("tail_lines") or 80))
            tail = "".join(lines[-n:])
    return {
        "job_id": _jid,
        "command": meta.get("command", ""),
        "pid": pid,
        "running": running,
        "exit_code": exit_code,
        "log_tail": tail,
        "log_bytes": log_bytes,
        "log_path": _log_path,
        "elapsed_sec": round(max(0.0, time.time() - float(meta.get("started_at", time.time()))), 2),
    }

if _act == "start":
    _cwd = _cfg.get("cwd") or "/content"
    if not os.path.isdir(_cwd):
        _cwd = "/tmp"
    _cmd = _cfg["command"]
    _runner = f"({_cmd}) > {_log_path!r} 2>&1; echo $? > {_exit_path!r}"
    proc = subprocess.Popen(["/bin/bash", "-lc", _runner], cwd=_cwd, start_new_session=True)
    with open(_meta_path, "w", encoding="utf-8") as f:
        json.dump({"job_id": _jid, "command": _cmd, "cwd": _cwd, "pid": proc.pid, "started_at": time.time()}, f)
    time.sleep(0.05)
    print(json.dumps(_read_status()))
elif _act == "poll":
    print(json.dumps(_read_status()))
elif _act == "kill":
    st = _read_status()
    pid = int(st["pid"])
    if st["running"] and pid > 0:
        try:
            os.killpg(pid, signal.SIGTERM)
        except Exception:
            try:
                os.kill(pid, signal.SIGTERM)
            except Exception:
                pass
        time.sleep(0.1)
        if _pid_alive(pid):
            try:
                os.killpg(pid, signal.SIGKILL)
            except Exception:
                pass
        if not os.path.exists(_exit_path):
            with open(_exit_path, "w", encoding="utf-8") as ef:
                ef.write("-15")
    print(json.dumps(_read_status()))
`, string(paramsJSON))
}
