package scripts

import (
	"encoding/json"
	"fmt"
)

// ScriptSaveNotebook commits all notebook modifications and cell outputs to Google Drive (Ctrl+S equivalent).
const ScriptSaveNotebook = `(async () => {
	const nb = window.colab?.global?.notebook;
	try {
		if (nb?.saveNotebook) {
			await nb.saveNotebook();
			return JSON.stringify({ success: true });
		}
		nb.commandHandler.execute({ id: "save", isDisabled: () => false });
		return JSON.stringify({ success: true });
	} catch (e) {
		return JSON.stringify({ error: e.toString() });
	}
})()`

// ScriptRunAll executes all cells in the notebook sequentially.
const ScriptRunAll = `(() => {
	const nb = window.colab?.global?.notebook;
	try {
		nb.commandHandler.execute({id: "runall", isDisabled: () => false});
		return JSON.stringify({ success: true });
	} catch (e) {
		if (nb?.runAll) {
			nb.runAll();
			return JSON.stringify({ success: true });
		}
		return JSON.stringify({ error: e.toString() });
	}
})()`

// ScriptRunCellBounded triggers execution of a code cell WITHOUT locking out Colab's UI or blocking indefinitely!
// If waitMs <= 0, it triggers execution and returns immediately once dispatched.
// If waitMs > 0 (default 8000ms), it waits up to waitMs for fast cells to finish and returns status="running"
// immediately if the cell is still running (e.g., multi-hour training), so MCP never hangs!
func ScriptRunCellBounded(cellID string, waitMs int) string {
	cfgJSON, _ := json.Marshal(map[string]any{
		"cell_id": cellID,
		"wait_ms": waitMs,
	})
	return fmt.Sprintf(`(async () => {
		// GOLAB_OP:run_code_cell %s
		const cfg = %s;
		const nb = window.colab?.global?.notebook;
		if (!nb) return JSON.stringify({ error: "No Colab notebook loaded." });

		const cells = nb.getCells ? nb.getCells() : [];
		let target = null;
		let cellIndex = -1;
		for (let i = 0; i < cells.length; i++) {
			const c = cells[i];
			const cid = c.cellId || (c.getCellId ? c.getCellId() : "") || (c.getId ? c.getId() : "");
			if (cid === cfg.cell_id) {
				target = c;
				cellIndex = i;
				break;
			}
		}
		if (!target) {
			return JSON.stringify({ error: "Cell not found: " + cfg.cell_id });
		}
		const cType = String(target.getType ? target.getType() : (target.model?.type || target.type || "code")).toLowerCase();
		if (cType && cType !== "code") {
			return JSON.stringify({ error: "Cell " + cfg.cell_id + " is not a code cell (type=" + cType + ")." });
		}

		const getExecState = (c) => {
			const st = String(c.model?.outputs?.executionState || "").toLowerCase();
			if (st === "running" || (c.isRunning && c.isRunning()) || c.busy) return "running";
			if (st === "pending" || (c.isPending && c.isPending())) return "pending";
			if (c.lastExecutionFailed) return "error";
			return "idle";
		};

		const extractOutputs = (c) => {
			let stdout = "";
			let stderr = "";
			let errText = "";
			const jsonCell = c.model?.toJson ? c.model.toJson() : null;
			const outs = Array.isArray(jsonCell?.outputs) ? jsonCell.outputs : [];
			for (const o of outs) {
				const ot = o?.output_type || "";
				if (ot === "stream") {
					const txt = Array.isArray(o.text) ? o.text.join("") : String(o.text || "");
					if (o.name === "stderr") stderr += txt;
					else stdout += txt;
				} else if (ot === "error") {
					errText += (o.ename || "Error") + ": " + (o.evalue || "") + "\n";
				}
			}
			return { stdout, stderr, error_text: errText.trim(), raw_outputs: outs };
		};

		// If already running/pending, do not re-trigger; report current live status immediately!
		const initialState = getExecState(target);
		const t0 = Date.now();
		if (initialState !== "running" && initialState !== "pending") {
			// Fire execution asynchronously without c.lockExecution() so long training never hangs MCP!
			if (typeof target.manualExecute === "function") {
				Promise.resolve(target.manualExecute(false)).catch(() => {});
			} else if (typeof target.autoExecute === "function") {
				Promise.resolve(target.autoExecute({})).catch(() => {});
			} else if (nb.colabMcpToolsService?.runCodeCellTool?.toolCallback) {
				Promise.resolve(nb.colabMcpToolsService.runCodeCellTool.toolCallback({ cellId: cfg.cell_id })).catch(() => {});
			} else {
				return JSON.stringify({ error: "Cell execution API unavailable on Colab frontend." });
			}
			// Give Colab ~120ms to transition model.outputs.executionState -> pending/running (or finish tiny 1ms cells)
			await new Promise(r => setTimeout(r, 120));
		}

		const maxWait = Math.max(0, Number(cfg.wait_ms || 0));
		while (Date.now() - t0 < maxWait) {
			const st = getExecState(target);
			if (st !== "running" && st !== "pending") {
				break;
			}
			await new Promise(r => setTimeout(r, 150));
		}

		const finalState = getExecState(target);
		const stillRunning = finalState === "running" || finalState === "pending";
		const out = extractOutputs(target);
		const execCount = target.getExecutionCount ? Number(target.getExecutionCount() || 0) : 0;

		return JSON.stringify({
			success: true,
			result: {
				cell_id: cfg.cell_id,
				index: cellIndex,
				status: stillRunning ? finalState : (target.lastExecutionFailed || out.error_text ? "error" : "completed"),
				is_running: stillRunning,
				execution_count: execCount,
				elapsed_seconds: Math.round((Date.now() - t0) / 100) / 10,
				stdout: out.stdout,
				stderr: out.stderr,
				error_text: out.error_text,
				outputs: out.raw_outputs,
				message: stillRunning
					? "Cell is actively executing in the background. Poll colab_get_running_cells or colab_get_cell_output to monitor progress."
					: "Cell execution finished."
			}
		});
	})()`, string(cfgJSON), string(cfgJSON))
}

// ScriptGetRunningCells inspects all cells and extracts true executionState ("running"/"pending"/"idle"),
// accurate elapsed seconds (computed ONLY when running or from output info.elapsed ms!), and live streaming stdout/stderr/errors.
const ScriptGetRunningCells = `(() => {
	const nb = window.colab?.global?.notebook;
	const cells = nb ? nb.getCells() : [];

	const running = [];
	for (let idx = 0; idx < cells.length; idx++) {
		const c = cells[idx];
		const cType = String(c.getType ? c.getType() : (c.model?.type || c.type || "code")).toLowerCase();
		if (cType && cType !== "code") continue;

		const rawExecState = String(c.model?.outputs?.executionState || "").toLowerCase();
		const isPending = rawExecState === "pending" || !!(c.isPending && c.isPending());
		const isRunning = rawExecState === "running" || !!(c.isRunning && c.isRunning()) || !!c.busy || isPending;
		const cellId = c.cellId || (c.getCellId ? c.getCellId() : "") || (c.getId ? c.getId() : "");

		let stdout = "";
		let latestStartMs = 0;
		let finishedElapsedMs = 0;
		let infoExecCount = 0;

		const outModel = c.model?.outputs || c.outputArea?.outputs;
		if (outModel?.outputs) {
			for (const outObj of Object.values(outModel.outputs)) {
				if (outObj?.executionStartTime && outObj.executionStartTime > latestStartMs) {
					latestStartMs = Number(outObj.executionStartTime);
				} else if (outObj?.creationTime && outObj.creationTime > latestStartMs) {
					latestStartMs = Number(outObj.creationTime);
				}
				if (outObj?.info?.elapsed !== undefined) {
					finishedElapsedMs = Number(outObj.info.elapsed || 0);
				}
				if (outObj?.info?.execution_count) {
					infoExecCount = Number(outObj.info.execution_count);
				}
				if (outObj?.outputItems) {
					for (const item of Object.values(outObj.outputItems)) {
						if (item?.text) {
							if (Array.isArray(item.text)) stdout += item.text.join("");
							else stdout += String(item.text);
						} else if (item?.output_type === "error") {
							stdout += (item.ename || "Error") + ": " + (item.evalue || "") + "\n";
						} else if (item?.data?.["text/plain"]) {
							const p = item.data["text/plain"];
							stdout += Array.isArray(p) ? p.join("") : String(p);
						}
					}
				}
			}
		}

		// Compute TRUE elapsed seconds:
		// - While running/pending: Date.now() - startTime (preferring outputObj.executionStartTime or cell.executionStateChangeTime)
		// - Once finished: outObj.info.elapsed / 1000 (NOT Date.now() - completionTime!)
		let elapsedSec = 0;
		if (isRunning) {
			const startTs = Number(c.executionStateChangeTime || latestStartMs || 0);
			if (startTs > 0) {
				elapsedSec = Math.max(0, Math.round((Date.now() - startTs) / 100) / 10);
			}
		} else if (finishedElapsedMs > 0) {
			elapsedSec = Math.round(finishedElapsedMs / 100) / 10;
		}

		const numCount = (c.getExecutionCount ? Number(c.getExecutionCount() || 0) : 0) || infoExecCount;
		let execStatusLabel = isPending ? "pending" : (isRunning ? "running" : "");
		if (!execStatusLabel) {
			if (c.lastExecutionFailed) execStatusLabel = numCount > 0 ? "[" + numCount + "] error" : "error";
			else if (numCount > 0) execStatusLabel = "[" + numCount + "]";
			else execStatusLabel = String(c.executionStatusText || "");
		} else if (numCount > 0) {
			execStatusLabel = "[" + numCount + "] " + execStatusLabel;
		}

		// Cap live stdout preview per cell to last 4000 chars so huge training logs never bloat /observe or MCP payloads
		let trimmed = stdout.trim();
		if (trimmed.length > 4000) {
			trimmed = "...(truncated)...\n" + trimmed.slice(-4000);
		}

		running.push({
			index: idx,
			cell_id: cellId,
			is_running: isRunning,
			elapsed_seconds: elapsedSec,
			execution_count: execStatusLabel,
			stdout: trimmed
		});
	}

	return JSON.stringify(running);
})()`

// ScriptCallMcpTool constructs JavaScript to execute one of Colab's native tools.
func ScriptCallMcpTool(field string, argsJSON string) string {
	return fmt.Sprintf(`(async () => {
		const nb = window.colab?.global?.notebook;
		const tools = nb?.colabMcpToolsService;
		if (!tools || !tools[%q]) {
			return JSON.stringify({ error: "Tool " + %q + " not found in colabMcpToolsService" });
		}

		try {
			const res = await tools[%q].toolCallback(%s);
			return JSON.stringify({ success: true, result: res });
		} catch (err) {
			return JSON.stringify({ error: err.toString(), stack: err.stack });
		}
	})()`, field, field, field, argsJSON)
}
