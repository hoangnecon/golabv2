package scripts

// ScriptCheckDriveFast performs a non-blocking DOM + /api/colab/drive XHR inspection
// that is 100% safe even while a primary training cell is busy.
const ScriptCheckDriveFast = `(async () => {
	// GOLAB_OP:check_drive_fast
	const stripXSSI = (text) => {
		let s = String(text || "");
		if (s.startsWith(")]}'\n")) s = s.substring(5);
		else if (s.startsWith(")]}'")) s = s.substring(4);
		return s.trim();
	};
	const withTimeout = (p, ms) => Promise.race([
		p,
		new Promise((_, reject) => setTimeout(() => reject(new Error("check_drive_fast timed out")), ms))
	]);

	const nb = window.colab?.global?.notebook;
	const kernel = nb?.getKernel ? nb.getKernel() : null;

	// Search File Browser shadow roots for the Mount/Unmount Drive icon button
	let buttonTitle = "";
	const findMountBtn = (root) => {
		if (!root || buttonTitle) return;
		if (root.nodeType === 1) {
			const aria = root.getAttribute?.("aria-label") || "";
			const title = root.getAttribute?.("title") || "";
			if (aria === "Mount Drive" || title === "Mount Drive" || title === "Unmount Drive") {
				buttonTitle = title || aria;
				return;
			}
			if (root.shadowRoot) findMountBtn(root.shadowRoot);
		}
		for (const child of root.childNodes || []) {
			findMountBtn(child);
		}
	};
	findMountBtn(document.body);

	const credsOK = !!(kernel?.areUserCredentialsAllowed ? kernel.areUserCredentialsAllowed() : false);
	let dfsMounted = false;
	const xhr = kernel?.getKernelXhr ? kernel.getKernelXhr() : null;
	if (xhr) {
		try {
			const resp = await withTimeout(xhr.fetch("/api/colab/drive", { method: "GET" }), 4000);
			if (resp?.ok) {
				const clean = stripXSSI(await resp.text());
				const parsed = clean ? JSON.parse(clean) : null;
				if (parsed && Array.isArray(parsed.dfs) && parsed.dfs.length > 0) {
					dfsMounted = true;
				}
			}
		} catch (_) {}
	}

	const btnSaysMounted = buttonTitle === "Unmount Drive";
	const mounted = dfsMounted && (btnSaysMounted || credsOK || buttonTitle === "");

	return JSON.stringify({
		mounted,
		status: mounted ? "mounted" : "unmounted",
		mount_path: "/content/drive",
		is_fuse_verified: false,
		button_state: buttonTitle,
		credentials_ok: credsOK,
		message: mounted
			? "Google Drive is mounted via Colab DFS."
			: "Google Drive is not mounted in this Colab runtime."
	});
})()`

// PythonCheckDriveFUSE inspects /proc/mounts and shutil.disk_usage for ground-truth FUSE verification.
const PythonCheckDriveFUSE = `import json, os, shutil
mounts = ""
try:
    with open("/proc/mounts", "r", encoding="utf-8") as f:
        mounts = f.read()
except Exception:
    pass
has_fuse = ("fuse.drive" in mounts) or (" /content/drive " in mounts)
is_mount = os.path.ismount("/content/drive")
dir_exists = os.path.exists("/content/drive")
non_empty = False
if dir_exists and os.path.isdir("/content/drive"):
    try:
        non_empty = len(os.listdir("/content/drive")) > 0
    except Exception:
        non_empty = False
out = {
    "has_fuse": bool(has_fuse),
    "is_mount": bool(is_mount),
    "dir_exists": bool(dir_exists),
    "non_empty": bool(non_empty),
}
if has_fuse or is_mount:
    target = "/content/drive/MyDrive" if os.path.exists("/content/drive/MyDrive") else "/content/drive"
    try:
        u = shutil.disk_usage(target)
        out["total"] = int(u.total)
        out["used"] = int(u.used)
        out["free"] = int(u.free)
    except Exception:
        pass
print(json.dumps(out))
`

// ScriptMountDriveProbe checks whether the current notebook is sole-authored or shared across accounts.
const ScriptMountDriveProbe = `(async () => {
	// GOLAB_OP:mount_drive_probe
	const withTimeout = (p, ms, fallback) => Promise.race([
		p,
		new Promise(r => setTimeout(() => r(fallback), ms))
	]);
	const nb = window.colab?.global?.notebook;
	if (!nb) return JSON.stringify({ error: "No Colab notebook loaded." });

	let isSole = false;
	if (nb.isSoleAuthored) {
		try {
			isSole = !!(await withTimeout(Promise.resolve(nb.isSoleAuthored()), 2500, false));
		} catch (_) {
			isSole = false;
		}
	}
	return JSON.stringify({ is_sole_authored: isSole });
})()`

// ScriptMountDriveSoleAuthored clicks the File Browser Mount Drive button (or calls nb.mountDrive())
// and confirms the <mwc-dialog open> Connect to Google Drive primary action button.
const ScriptMountDriveSoleAuthored = `(async () => {
	// GOLAB_OP:mount_drive_sole
	const nb = window.colab?.global?.notebook;
	if (!nb) return JSON.stringify({ error: "No Colab notebook loaded." });

	let clickedMount = false;
	const findAndClickMountBtn = (root) => {
		if (!root || clickedMount) return;
		if (root.nodeType === 1) {
			const aria = root.getAttribute?.("aria-label") || "";
			const title = root.getAttribute?.("title") || "";
			if (title === "Mount Drive" || (aria === "Mount Drive" && title !== "Unmount Drive")) {
				root.click();
				clickedMount = true;
				return;
			}
			if (root.shadowRoot) findAndClickMountBtn(root.shadowRoot);
		}
		for (const child of root.childNodes || []) {
			findAndClickMountBtn(child);
		}
	};
	findAndClickMountBtn(document.body);

	if (!clickedMount && nb.mountDrive) {
		nb.mountDrive();
		clickedMount = true;
	}

	let confirmedDialog = false;
	const t0 = Date.now();
	while (Date.now() - t0 < 5000) {
		await new Promise(r => setTimeout(r, 150));
		const mwc = document.querySelector("mwc-dialog[open]");
		if (mwc) {
			const okBtn = mwc.querySelector("[slot='primaryAction']") ||
				Array.from(mwc.querySelectorAll("md-text-button, button")).find(b => (b.innerText || "").includes("Connect"));
			if (okBtn) {
				okBtn.click();
				confirmedDialog = true;
				break;
			}
		}
	}

	return JSON.stringify({ clicked_mount: clickedMount, confirmed_dialog: confirmedDialog });
})()`

// ScriptMountDriveSharedTrigger starts ephemeral Drive mount for shared notebooks without polluting cells,
// and clicks Colab's in-page Connect to Google Drive dialog so the OAuth popup opens.
const ScriptMountDriveSharedTrigger = `(async () => {
	// GOLAB_OP:mount_drive_shared_trigger
	const nb = window.colab?.global?.notebook;
	const kernel = nb?.getKernel ? nb.getKernel() : null;
	if (!kernel) {
		return JSON.stringify({ error: "Runtime kernel must be connected before mounting Drive on a shared notebook." });
	}

	// Fire ephemeral mount asynchronously (do not await completion before clicking the modal dialog!)
	try {
		if (kernel.execute) {
			Promise.resolve(kernel.execute("from google.colab import drive\ndrive._mount('/content/drive', force_remount=False)", undefined, {})).catch(() => {});
		} else if (nb.mountDrive) {
			nb.mountDrive();
		}
	} catch (_) {}

	let confirmedDialog = false;
	const t0 = Date.now();
	while (Date.now() - t0 < 5000) {
		await new Promise(r => setTimeout(r, 150));
		const mwc = document.querySelector("mwc-dialog[open]");
		if (mwc) {
			const okBtn = mwc.querySelector("[slot='primaryAction']") ||
				Array.from(mwc.querySelectorAll("md-text-button, button")).find(b => (b.innerText || "").includes("Connect"));
			if (okBtn) {
				okBtn.click();
				confirmedDialog = true;
				break;
			}
		}
	}

	return JSON.stringify({ triggered: true, confirmed_dialog: confirmedDialog });
})()`

// ScriptAutoConsentOAuthPopup attempts a safe click on the matching signed-in account / Allow button inside an OAuth popup.
const ScriptAutoConsentOAuthPopup = `(async () => {
	// GOLAB_OP:oauth_popup_consent
	const clickMatching = (selectors) => {
		for (const sel of selectors) {
			const el = document.querySelector(sel);
			if (el && !el.disabled) {
				el.click();
				return true;
			}
		}
		return false;
	};

	let acted = false;
	// Step 1: Account chooser tile (if single signed-in profile is listed)
	const acctItems = document.querySelectorAll("[data-identifier]");
	if (acctItems.length === 1) {
		acctItems[0].click();
		acted = true;
		await new Promise(r => setTimeout(r, 800));
	}

	// Step 2: Continue / Allow button
	const buttons = Array.from(document.querySelectorAll("button"));
	for (const b of buttons) {
		const txt = (b.innerText || "").trim().toLowerCase();
		if (txt === "continue" || txt === "allow" || txt === "tiếp tục" || txt === "cho phép") {
			b.click();
			acted = true;
			break;
		}
	}
	if (!acted) {
		acted = clickMatching(["#submit_approve_access"]);
	}
	return JSON.stringify({ acted, url: String(window.location?.href || "") });
})()`
