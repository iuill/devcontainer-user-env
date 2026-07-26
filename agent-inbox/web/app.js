const gallery = document.querySelector("#gallery");
const emptyState = document.querySelector("#empty-state");
const statusElement = document.querySelector("#status");
const dropZone = document.querySelector("#drop-zone");
const chooseButton = document.querySelector("#choose-button");
const fileInput = document.querySelector("#file-input");
const template = document.querySelector("#image-card-template");
const dialog = document.querySelector("#preview-dialog");
const previewImage = document.querySelector("#preview-image");
const textInput = document.querySelector("#text-input");
const textSize = document.querySelector("#text-size");
const sendTextButton = document.querySelector("#send-text-button");
const clipboardButton = document.querySelector("#clipboard-button");
let uploading = false;
let dragDepth = 0;
let maxBytes = 20 * 1024 * 1024;

chooseButton.addEventListener("click", () => fileInput.click());
sendTextButton.addEventListener("click", uploadTextInput);
clipboardButton.addEventListener("click", pasteClipboardText);
textInput.addEventListener("input", updateTextSize);
document.querySelector("#refresh-button").addEventListener("click", loadItems);
document.querySelector("#close-preview").addEventListener("click", closePreview);
dialog.addEventListener("click", (event) => {
  if (event.target === dialog) closePreview();
});
dialog.addEventListener("close", () => {
  previewImage.removeAttribute("src");
});

fileInput.addEventListener("change", () => {
  uploadFiles([...fileInput.files]);
  fileInput.value = "";
});

document.addEventListener("paste", (event) => {
  if (uploading || !event.clipboardData) return;
  const files = [...event.clipboardData.items]
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter(Boolean);
  if (event.target === textInput) {
    if (files.length) {
      event.preventDefault();
      uploadFiles(files);
    }
    return;
  }
  if (files.length) {
    event.preventDefault();
    uploadFiles(files);
    return;
  }
  const text = event.clipboardData.getData("text/plain");
  if (text) {
    event.preventDefault();
    uploadEntries([{kind: "text", value: text}]);
  }
});

dropZone.addEventListener("dragenter", (event) => {
  event.preventDefault();
  dragDepth += 1;
  dropZone.classList.add("dragging");
});
dropZone.addEventListener("dragover", (event) => event.preventDefault());
dropZone.addEventListener("dragleave", (event) => {
  event.preventDefault();
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) dropZone.classList.remove("dragging");
});
dropZone.addEventListener("drop", (event) => {
  event.preventDefault();
  dragDepth = 0;
  dropZone.classList.remove("dragging");
  uploadFiles([...event.dataTransfer.files]);
});

async function request(url, options = {}) {
  const response = await fetch(url, options);
  if (!response.ok) {
    let message = `エラーが発生しました (${response.status})`;
    try {
      const body = await response.json();
      if (body.error) message = body.error;
    } catch {
      // Keep the generic error message.
    }
    throw new Error(message);
  }
  return response.status === 204 ? null : response.json();
}

async function uploadFiles(files) {
  const entries = files.map((file) => {
    if (file.type.startsWith("image/")) return {kind: "image", value: file};
    if (file.type === "text/plain" || file.name.toLowerCase().endsWith(".txt")) {
      return {kind: "textFile", value: file};
    }
    return {kind: "unsupported", value: file.name};
  });
  uploadEntries(entries);
}

async function uploadEntries(entries) {
  if (uploading) {
    showStatus("現在の保存が完了してからもう一度お試しください", true);
    return 0;
  }
  if (!entries.length) {
    showStatus("画像またはテキストが見つかりませんでした", true);
    return 0;
  }
  const oversized = entries.find((entry) => entrySize(entry) > maxBytes);
  if (oversized) {
    showStatus(`1ファイルの上限は${formatBytes(maxBytes)}です`, true);
    return 0;
  }

  uploading = true;
  chooseButton.disabled = true;
  sendTextButton.disabled = true;
  clipboardButton.disabled = true;
  dropZone.classList.add("busy");
  showStatus(`${entries.length}件のファイルを保存しています…`);

  const results = [];
  for (const entry of entries) {
    try {
      results.push({status: "fulfilled", value: await uploadEntry(entry)});
    } catch (error) {
      results.push({status: "rejected", reason: error});
    }
  }

  uploading = false;
  chooseButton.disabled = false;
  clipboardButton.disabled = false;
  dropZone.classList.remove("busy");
  updateTextSize();
  const succeeded = results.filter((result) => result.status === "fulfilled").length;
  const failed = results.length - succeeded;
  if (failed) {
    const firstError = results.find((result) => result.status === "rejected").reason;
    showStatus(`${results.length}件中${succeeded}件を保存、${failed}件失敗: ${firstError.message}`, true);
  } else {
    showStatus(`${succeeded}件のファイルを保存しました`);
  }
  if (succeeded) await loadItems();
  return succeeded;
}

function uploadEntry(entry) {
  if (entry.kind === "image") {
    const form = new FormData();
    form.append("image", entry.value, entry.value.name || "clipboard.png");
    return request("/api/images", {
      method: "POST",
      headers: {"X-Agent-Inbox": "1"},
      body: form,
    });
  }
  if (entry.kind === "text" || entry.kind === "textFile") {
    return request("/api/texts", {
      method: "POST",
      headers: {
        "Content-Type": "text/plain; charset=utf-8",
        "X-Agent-Inbox": "1",
      },
      body: entry.value,
    });
  }
  return Promise.reject(new Error(`${entry.value} は対応していないファイル形式です`));
}

async function uploadTextInput() {
  const text = textInput.value;
  if (!text) {
    showStatus("共有するテキストを入力してください", true);
    textInput.focus();
    return;
  }
  if (await uploadEntries([{kind: "text", value: text}])) {
    textInput.value = "";
    updateTextSize();
  }
}

async function pasteClipboardText() {
  try {
    textInput.value = await navigator.clipboard.readText();
    updateTextSize();
    textInput.focus();
  } catch {
    showStatus("自動で読み取れませんでした。入力欄を長押しして貼り付けてください", true);
    textInput.focus();
  }
}

function entrySize(entry) {
  if (entry.kind === "text") return new Blob([entry.value]).size;
  if (entry.kind === "image" || entry.kind === "textFile") return entry.value.size;
  return 0;
}

function updateTextSize() {
  const size = new Blob([textInput.value]).size;
  textSize.textContent = `${formatBytes(size)} / ${formatBytes(maxBytes)}`;
  textSize.classList.toggle("over-limit", size > maxBytes);
  sendTextButton.disabled = uploading || size > maxBytes;
}

async function loadItems() {
  try {
    const items = await request("/api/items");
    gallery.replaceChildren();
    emptyState.textContent = "まだ共有ファイルがありません。";
    emptyState.hidden = items.length !== 0;
    for (const item of items) gallery.append(createCard(item));
  } catch (error) {
    gallery.replaceChildren();
    emptyState.textContent = "共有ファイルの読み込みに失敗しました。";
    emptyState.hidden = false;
    showStatus(error.message, true);
  }
}

function createCard(item) {
  const card = template.content.firstElementChild.cloneNode(true);
  const img = card.querySelector("img");
  const textPreview = card.querySelector(".text-preview");
  if (item.kind === "image") {
    img.src = item.url;
    img.alt = item.name;
    if (item.width && item.height) {
      img.width = item.width;
      img.height = item.height;
    }
    card.querySelector(".preview").setAttribute("aria-label", `${item.name} を拡大`);
    textPreview.hidden = true;
  } else {
    img.remove();
    textPreview.hidden = false;
    textPreview.textContent = item.snippet || "TXT";
    textPreview.id = `snippet-${item.name}`;
    card.querySelector(".preview").setAttribute("aria-label", `${item.name} を開く`);
    card.querySelector(".preview").setAttribute("aria-describedby", textPreview.id);
  }
  card.querySelector(".path").textContent = item.path;
  card.querySelector(".metadata").textContent =
    `${item.kind === "text" ? "TEXT" : "IMAGE"} · ${formatBytes(item.size)} · ${new Date(item.time).toLocaleString()}`;

  card.querySelector(".preview").addEventListener("click", () => {
    if (item.kind === "text") {
      window.open(item.url, "_blank", "noopener");
      return;
    }
    previewImage.src = item.url;
    dialog.showModal();
  });
  card.querySelector(".copy-button").addEventListener("click", async (event) => {
    try {
      await copyText(item.path);
      const button = event.currentTarget;
      button.textContent = "コピーしました";
      setTimeout(() => { button.textContent = "パスをコピー"; }, 1200);
    } catch {
      window.prompt("次のパスをコピーしてください", item.path);
    }
  });
  card.querySelector(".delete-button").addEventListener("click", async () => {
    if (!confirm(`${item.name} を削除しますか？`)) return;
    try {
      await request(`/api/items/${encodeURIComponent(item.name)}`, {
        method: "DELETE",
        headers: {"X-Agent-Inbox": "1"},
      });
      card.remove();
      emptyState.hidden = gallery.children.length !== 0;
      showStatus("共有ファイルを削除しました");
    } catch (error) {
      showStatus(error.message, true);
    }
  });
  return card;
}

function closePreview() {
  if (dialog.open) dialog.close();
  previewImage.removeAttribute("src");
}

async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Fall back for browsers that expose the API but deny clipboard access.
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.readOnly = true;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("clipboard copy failed");
}

function showStatus(message, isError = false) {
  statusElement.textContent = message;
  statusElement.classList.toggle("error", isError);
  statusElement.classList.add("visible");
  clearTimeout(showStatus.timeout);
  showStatus.timeout = setTimeout(() => statusElement.classList.remove("visible"), 4500);
}

function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}

async function initialize() {
  try {
    const config = await request("/api/config");
    maxBytes = config.maxBytes;
    updateTextSize();
  } catch (error) {
    showStatus(error.message, true);
  }
  await loadItems();
}

initialize();
