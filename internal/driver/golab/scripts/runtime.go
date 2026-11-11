package scripts

import "fmt"

// ScriptProbeTabState inspects the attached browser tab for Google sign-in redirect,
// Drive permission denial, notebook identity/permissions, and layered runtime/kernel state.
const ScriptProbeTabState = `(async () => {
	// GOLAB_OP:probe_tab_state
	const withTimeout = (p, ms, fallback) => Promise.race([
		p,
		new Promise(resolve => setTimeout(() => resolve(fallback), ms))
	]);

	const href = String(window.location?.href || "");
	const title = String(document.title || "");

	if (href.includes("accounts.google.com")) {
		return JSON.stringify({
			url: href,
			title,
			connection_state: "auth_required",
			permission: "auth_required",
			connect_state: "disconnected",
			kernel_state: "",
			is_executing: false,
			identity: {
				is_authenticated: false,
				permission: "auth_required"
			}
		});
	}

	const bodyText = String(document.body?.innerText || "");
	const nb = window.colab?.global?.notebook;
	const nm = window.colab?.global?.notebookModel || nb?.model;

	const userInfo = nm?.userInfo || {};
	let email = userInfo.emailAddress || "";
	let displayName = userInfo.displayName || "";
	if (!email) {
		const acctLink = document.querySelector('a[aria-label*="Google Account:"]');
		if (acctLink) {
			const label = acctLink.getAttribute("aria-label") || "";
			const m = label.match(/\(([^)]+@[^)]+)\)/);
			if (m) email = m[1];
		}
	}

	if (!nb && (bodyText.includes("You need access") || bodyText.includes("Request access") || title.includes("You need access"))) {
		return JSON.stringify({
			url: href,
			title,
			connection_state: "permission_denied",
			permission: "denied",
			connect_state: "disconnected",
			kernel_state: "",
			is_executing: false,
			identity: {
				email_address: email,
				display_name: displayName,
				is_authenticated: !!email,
				permission: "denied"
			}
		});
	}

	const driveMeta = nm?.driveMetadata || {};
	const rawRole = String(driveMeta?.userPermission?.role || "").toLowerCase();
	const perms = (nm?.getPermissions ? nm.getPermissions() : null) || {};
	const editable = typeof perms.editable === "boolean" ? perms.editable : (nb?.isEditable ? !!nb.isEditable() : false);
	const saveable = typeof perms.saveable === "boolean" ? perms.saveable : editable;

	let permission = "unknown";
	if (rawRole === "owner") permission = "owner";
	else if (rawRole === "writer" || editable) permission = "editor";
	else if (rawRole === "commenter" || perms.commentable) permission = "commenter";
	else if (rawRole === "reader" || nb) permission = "view_only";

	if (bodyText.includes("You need access") && !nb) {
		permission = "denied";
	}

	let isSoleAuthored = false;
	if (nb?.isSoleAuthored) {
		try {
			isSoleAuthored = !!(await withTimeout(Promise.resolve(nb.isSoleAuthored()), 1500, false));
		} catch (_) {
			isSoleAuthored = false;
		}
	}

	const btn = document.querySelector("colab-connect-button");
	const connectState = String(btn?.connectState || "disconnected").toLowerCase();
	const kernel = nb?.getKernel ? nb.getKernel() : null;
	const kernelState = String(kernel?.getState ? kernel.getState() : (kernel?.state || "")).toLowerCase();
	const isExecuting = !!(nb?.isExecuting ? nb.isExecuting() : false) || kernelState.includes("busy");

	let connState = "runtime_disconnected";
	if (permission === "denied") {
		connState = "permission_denied";
	} else if (permission === "auth_required") {
		connState = "auth_required";
	} else if (connectState === "allocating" || connectState === "connecting" || connectState === "initializing" || kernelState.includes("starting") || kernelState.includes("connecting")) {
		connState = "connecting";
	} else if (connectState === "connected" || kernelState.includes("idle") || kernelState.includes("busy")) {
		connState = isExecuting ? "busy" : "idle";
	}

	let accelName = "";
	let runtimeShape = "";
	let uptimeSec = 0;
	try {
		const ep = kernel?.getEndpoint ? kernel.getEndpoint() : null;
		accelName = ep?.acceleratorModel || ep?.accelerator || "";
		runtimeShape = ep?.shape || "";
		if (kernel?.connectionTime) {
			uptimeSec = Math.max(0, Math.floor((Date.now() - Number(kernel.connectionTime)) / 1000));
		}
	} catch (_) {}

	return JSON.stringify({
		url: href,
		title: driveMeta.title || title,
		connection_state: connState,
		permission,
		connect_state: connectState,
		kernel_state: kernelState,
		is_executing: isExecuting,
		identity: {
			email_address: email,
			display_name: displayName,
			is_authenticated: !!(userInfo.isAuthenticatedUser || email),
			drive_file_id: driveMeta.id || "",
			title: driveMeta.title || title,
			role: rawRole,
			permission,
			editable,
			saveable,
			is_sole_authored: isSoleAuthored,
			has_other_writers: !!(nm?.hasOtherWriters ? nm.hasOtherWriters() : false),
			has_merge_conflict: !!(nm?.hasMergeConflict ? nm.hasMergeConflict() : false),
			has_unsaved_edits: !!(nb?.hasUnsavedFileEdits ? nb.hasUnsavedFileEdits() : false),
			accelerator_name: String(accelName || ""),
			runtime_uptime_seconds: uptimeSec,
			runtime_shape: String(runtimeShape || "")
		}
	});
})()`

// ScriptGetComputeUnits extracts account Compute Units balance, burn rate, tier, and eligible GPUs/TPUs.
const ScriptGetComputeUnits = `(async () => {
	// GOLAB_OP:get_compute_units
	const withTimeout = (p, ms) => Promise.race([
		p,
		new Promise((_, reject) => setTimeout(() => reject(new Error("fetchCcuInfo timed out")), ms))
	]);

	const nb = window.colab?.global?.notebook;
	if (!nb) {
		return JSON.stringify({ error: "Colab notebook instance is not initialized on this tab." });
	}

	let tier = "free";
	try {
		const sub = nb.getSubscription ? nb.getSubscription() : null;
		const t = sub?.getTier ? Number(sub.getTier()) : 0;
		if (t === 1) tier = "pro";
		else if (t >= 2) tier = "pro_plus";
	} catch (_) {}

	const prm = nb.appModel?.paidResourceMonitor;
	let info = prm?.cache || null;
	if (prm?.fetchCcuInfo) {
		try {
			info = await withTimeout(prm.fetchCcuInfo(), 6000);
		} catch (_) {
			info = prm?.cache || info;
		}
	}
	if (!info) {
		return JSON.stringify({
			assignments_count: 0,
			balance: 0,
			consumption_rate_hourly: 0,
			formatted_balance: "0.00",
			tier,
			eligible_gpus: [],
			ineligible_gpus: [],
			eligible_tpus: [],
			ineligible_tpus: []
		});
	}

	const bal = Number(info.balance || 0);
	return JSON.stringify({
		assignments_count: Number(info.assignmentsCount || 0),
		balance: bal,
		consumption_rate_hourly: Number(info.consumptionRateHourly || 0),
		formatted_balance: String(info.formattedBalance || bal.toFixed(2)),
		tier,
		eligible_gpus: Array.isArray(info.eligibleGpus) ? info.eligibleGpus.map(String) : [],
		ineligible_gpus: Array.isArray(info.ineligibleGpus) ? info.ineligibleGpus.map(String) : [],
		eligible_tpus: Array.isArray(info.eligibleTpus) ? info.eligibleTpus.map(String) : [],
		ineligible_tpus: Array.isArray(info.ineligibleTpus) ? info.ineligibleTpus.map(String) : []
	});
})()`

// ScriptListAccountVMSessions lists all active Colab VM assignments across the signed-in Google Account.
const ScriptListAccountVMSessions = `(async () => {
	// GOLAB_OP:list_vm_sessions
	const withTimeout = (p, ms) => Promise.race([
		p,
		new Promise((_, reject) => setTimeout(() => reject(new Error("listNotebookSessions timed out")), ms))
	]);

	const nb = window.colab?.global?.notebook;
	const kernel = nb?.getKernel ? nb.getKernel() : null;
	if (!kernel?.listNotebookSessions) {
		return JSON.stringify([]);
	}

	try {
		const raw = await withTimeout(kernel.listNotebookSessions(), 6000);
		const arr = Array.isArray(raw) ? raw : [];
		return JSON.stringify(arr.map(item => {
			const ep = item?.tunnelEndpoint || {};
			const proxy = ep?.runtimeProxyInfo || {};
			const accel = item?.accelerator || {};
			return {
				file_id: String(item?.fileId || ""),
				title: String(item?.title || ""),
				kernel_id: String(item?.kernelId || ""),
				session_id: String(item?.sessionId || ""),
				accelerator: String(accel?.display_name || accel?.name || ""),
				accelerator_model: String(ep?.acceleratorModel || ""),
				shape: String(ep?.shape || ""),
				managed_id: String(ep?.managedId || ""),
				proxy_url: String(proxy?.url || ""),
				token_expires_sec: Number(proxy?.tokenExpiresInSeconds || 0)
			};
		}));
	} catch (err) {
		return JSON.stringify({ error: err.toString() });
	}
})()`

// ScriptConnectRuntime triggers connection to the Colab hosted runtime and waits for allocation.
const ScriptConnectRuntime = `(async () => {
	// GOLAB_OP:connect_runtime
	const nb = window.colab?.global?.notebook;
	const btn = document.querySelector("colab-connect-button");
	if (btn?.connectState === "connected") {
		return JSON.stringify({ success: true, message: "already_connected" });
	}

	try {
		if (nb?.connectToKernel) {
			nb.connectToKernel();
		} else {
			nb.commandHandler.execute({id: "connect", isDisabled: () => false});
		}
	} catch (e) {
		const inner = btn?.shadowRoot?.querySelector("#connect");
		if (inner) inner.click();
		else return JSON.stringify({ error: e.toString() });
	}

	const t0 = Date.now();
	while (Date.now() - t0 < 35000) {
		await new Promise(r => setTimeout(r, 500));
		if (btn?.connectState === "connected") {
			return JSON.stringify({ success: true, connectState: btn.connectState });
		}
	}
	return JSON.stringify({ error: "Connect timed out after 35 seconds", connectState: btn?.connectState });
})()`

// ScriptDisconnectRuntime powerwashes and releases the active VM instance.
const ScriptDisconnectRuntime = `(async () => {
	// GOLAB_OP:disconnect_runtime
	const nb = window.colab?.global?.notebook;
	const kernel = nb?.getKernel ? nb.getKernel() : null;
	const btn = document.querySelector("colab-connect-button");

	if (btn?.connectState === "disconnected") {
		return JSON.stringify({ success: true, message: "already_disconnected" });
	}

	try {
		nb.commandHandler.execute({id: "powerwash-current-vm", isDisabled: () => false});
	} catch (e) {
		if (kernel?.unassignCurrentVm) {
			await kernel.unassignCurrentVm();
		} else {
			return JSON.stringify({ error: e.toString() });
		}
	}

	const t0 = Date.now();
	while (Date.now() - t0 < 3000) {
		await new Promise(r => setTimeout(r, 150));
		const mwc = document.querySelector("mwc-dialog[open]");
		if (mwc) {
			const yesBtn = mwc.querySelector("[slot='primaryAction']") || Array.from(mwc.querySelectorAll("button, md-text-button")).find(b => b.innerText?.trim() === "Yes");
			if (yesBtn) {
				yesBtn.click();
				break;
			}
		}
	}

	const t1 = Date.now();
	while (Date.now() - t1 < 5000) {
		await new Promise(r => setTimeout(r, 250));
		if (btn?.connectState === "disconnected") {
			return JSON.stringify({ success: true });
		}
	}
	return JSON.stringify({ success: true });
})()`

// ScriptGetAvailableAccelerators reads the hardware accelerator options dynamically from Colab runtime settings.
const ScriptGetAvailableAccelerators = `(async () => {
	// GOLAB_OP:get_accelerators
	const nb = window.colab?.global?.notebook;
	try {
		nb.commandHandler.execute({id: "change-runtime-type", isDisabled: () => false});
	} catch (e) {
		return JSON.stringify({ error: e.toString() });
	}

	await new Promise(r => setTimeout(r, 500));

	const mwc = document.querySelector("mwc-dialog[open]");
	if (!mwc) return JSON.stringify({ error: "Dialog failed to open" });

	const accelerators = [];
	const walk = (node) => {
		if (!node) return;
		if (node.nodeType === 1) {
			const tag = node.tagName.toLowerCase();
			if (tag === "input" && (node.name === "accelerator" || node.getAttribute("name") === "accelerator")) {
				accelerators.push({
					name: node.getAttribute("aria-label") || node.value || "CPU",
					value: node.value,
					checked: node.checked,
					disabled: node.disabled || node.hasAttribute("disabled")
				});
			}
			if (node.shadowRoot) walk(node.shadowRoot);
		}
		for (const child of node.childNodes || []) {
			walk(child);
		}
	};
	walk(mwc);

	const highRamInput = mwc.querySelector("input#switch") || mwc.querySelector("input[aria-label='High-RAM']");
	const highRamChecked = highRamInput ? highRamInput.checked : false;

	const cancelBtn = mwc.querySelector("[slot='secondaryAction']");
	if (cancelBtn) cancelBtn.click();
	else mwc.open = false;

	return JSON.stringify({
		accelerators: accelerators,
		high_ram: highRamChecked
	});
})()`

// ScriptSetAccelerator selects a specific accelerator and High-RAM configuration.
func ScriptSetAccelerator(value string, highRAM bool) string {
	return fmt.Sprintf(`(async () => {
		// GOLAB_OP:set_accelerator
		const nb = window.colab?.global?.notebook;
		try {
			nb.commandHandler.execute({id: "change-runtime-type", isDisabled: () => false});
		} catch (e) {
			return JSON.stringify({ error: e.toString() });
		}

		await new Promise(r => setTimeout(r, 500));

		const mwc = document.querySelector("mwc-dialog[open]");
		if (!mwc) return JSON.stringify({ error: "Dialog failed to open" });

		const targetVal = %q;
		let found = false;
		const walk = (node) => {
			if (!node) return;
			if (node.nodeType === 1) {
				const tag = node.tagName.toLowerCase();
				if (tag === "input" && (node.name === "accelerator" || node.getAttribute("name") === "accelerator")) {
					if (node.value === targetVal) {
						if (node.disabled || node.hasAttribute("disabled")) {
							throw new Error("Accelerator " + targetVal + " is disabled for this account plan.");
						}
						node.checked = true;
						node.dispatchEvent(new Event("change", { bubbles: true }));
						node.click();
						found = true;
					}
				}
				if (node.shadowRoot) walk(node.shadowRoot);
			}
			for (const child of node.childNodes || []) {
				walk(child);
			}
		};
		try {
			walk(mwc);
		} catch (err) {
			const cancel = mwc.querySelector("[slot='secondaryAction']");
			if (cancel) cancel.click();
			return JSON.stringify({ error: err.message });
		}

		if (!found) {
			const cancel = mwc.querySelector("[slot='secondaryAction']");
			if (cancel) cancel.click();
			return JSON.stringify({ error: "Accelerator value not found: " + targetVal });
		}

		const highRamInput = mwc.querySelector("input#switch") || mwc.querySelector("input[aria-label='High-RAM']");
		if (highRamInput && %t !== highRamInput.checked) {
			highRamInput.click();
		}

		const saveBtn = mwc.querySelector("[slot='primaryAction']");
		if (saveBtn) {
			saveBtn.click();
		}

		return JSON.stringify({ success: true, selected: targetVal });
	})()`, value, highRAM)
}

// ScriptInterruptKernel dispatches native SIGINT to the active kernel.
const ScriptInterruptKernel = `(() => {
	// GOLAB_OP:interrupt_kernel
	const nb = window.colab?.global?.notebook;
	const kernel = nb?.getKernel ? nb.getKernel() : null;
	if (kernel?.interrupt) {
		kernel.interrupt();
		return JSON.stringify({ success: true });
	}
	if (kernel?.connection?.interrupt) {
		kernel.connection.interrupt();
		return JSON.stringify({ success: true });
	}
	return JSON.stringify({ error: "kernel.interrupt is not available" });
})()`

// ScriptRestartKernel triggers Colab's frontend kernel/connection restart() and confirms any mwc-dialog prompt.
const ScriptRestartKernel = `(async () => {
	// GOLAB_OP:restart_kernel
	const nb = window.colab?.global?.notebook;
	const kernel = nb?.getKernel ? nb.getKernel() : null;
	const restartFn = kernel?.restart
		? () => kernel.restart()
		: (kernel?.connection?.restart ? () => kernel.connection.restart() : null);
	if (!restartFn) {
		return JSON.stringify({ error: "kernel.restart is not available" });
	}
	try {
		const restartPromise = Promise.resolve(restartFn());
		const t0 = Date.now();
		while (Date.now() - t0 < 2000) {
			await new Promise(r => setTimeout(r, 120));
			const mwc = document.querySelector("mwc-dialog[open]");
			if (mwc) {
				const yesBtn = mwc.querySelector("[slot='primaryAction']") ||
					Array.from(mwc.querySelectorAll("button, md-text-button")).find(b => /yes|restart/i.test(b.innerText?.trim() || ""));
				if (yesBtn) {
					yesBtn.click();
					break;
				}
			}
		}
		await Promise.race([restartPromise, new Promise(r => setTimeout(r, 4000))]);
		return JSON.stringify({ success: true });
	} catch (err) {
		return JSON.stringify({ error: err.toString() });
	}
})()`

// ScriptGetProxyURL resolves Colab's external proxy URL and runtime bearer token for an exposed VM port.
func ScriptGetProxyURL(port int) string {
	return fmt.Sprintf(`(async () => {
		// GOLAB_OP:get_proxy_url
		const withTimeout = (p, ms) => Promise.race([
			p,
			new Promise((_, reject) => setTimeout(() => reject(new Error("proxyPort timed out")), ms))
		]);
		const port = %d;
		const nb = window.colab?.global?.notebook;
		const kernel = nb?.getKernel ? nb.getKernel() : null;
		if (!kernel) {
			return JSON.stringify({ error: "No active Colab kernel found." });
		}

		let url = "";
		if (nb?.getProxyUrlForPort) {
			url = String(await withTimeout(nb.getProxyUrlForPort(port), 5000) || "");
		} else if (kernel?.proxyPort) {
			url = String(await withTimeout(kernel.proxyPort(port, {}), 5000) || "");
		}

		let token = "";
		let expSec = 0;
		const ep = kernel?.getEndpoint ? kernel.getEndpoint() : (kernel?.endpoint || null);
		const proxyInfo = ep?.getRuntimeProxyInfo ? await Promise.resolve(ep.getRuntimeProxyInfo()) : ep?.runtimeProxyInfo;
		if (proxyInfo) {
			token = String(proxyInfo.token || "");
			expSec = Number(proxyInfo.tokenExpiresInSeconds || 0);
		}
		const xhr = (kernel?.getKernelXhr ? kernel.getKernelXhr() : null) || kernel?.xhr || (kernel?.connection?.getKernelXhr ? kernel.connection.getKernelXhr() : null);
		if (!token && xhr?.getRuntimeProxyToken) {
			token = String(await withTimeout(xhr.getRuntimeProxyToken(port), 4000) || "");
		}

		return JSON.stringify({
			port,
			url,
			token,
			token_expires_sec: expSec
		});
	})()`, port)
}
