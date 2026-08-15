const gallery = document.querySelector("#gallery");
const emptyState = document.querySelector("#empty-state");
const statusElement = document.querySelector("#status");
const dropZone = document.querySelector("#drop-zone");
const chooseButton = document.querySelector("#choose-button");
const fileInput = document.querySelector("#file-input");
const template = document.querySelector("#image-card-template");
const dialog = document.querySelector("#preview-dialog");
const previewImage = document.querySelector("#preview-image");
const markdownView = document.querySelector("#markdown-view");
const markdownTitle = document.querySelector("#markdown-title");
const markdownRawLink = document.querySelector("#markdown-raw-link");
const markdownContent = document.querySelector("#markdown-content");
const textInput = document.querySelector("#text-input");
const textSize = document.querySelector("#text-size");
const sendTextButton = document.querySelector("#send-text-button");
const clipboardButton = document.querySelector("#clipboard-button");
const savedItem = document.querySelector("#saved-item");
const savedHostPath = document.querySelector("#saved-host-path");
const savedDevPath = document.querySelector("#saved-dev-path");
const savedMessage = document.querySelector("#saved-message");
const savedHostCopyButton = document.querySelector("#saved-host-copy-button");
const savedDevCopyButton = document.querySelector("#saved-dev-copy-button");
const selectAllButton = document.querySelector("#select-all-button");
const deleteSelectedButton = document.querySelector("#delete-selected-button");
const deleteAllButton = document.querySelector("#delete-all-button");
const selectionStatus = document.querySelector("#selection-status");
const refreshButton = document.querySelector("#refresh-button");
const sourceRootElement = document.querySelector("#source-root");
const sourceBreadcrumbs = document.querySelector("#source-breadcrumbs");
const sourceGrid = document.querySelector("#source-grid");
const sourceEmptyState = document.querySelector("#source-empty-state");
const sourceRefreshButton = document.querySelector("#source-refresh-button");
const sourceTemplate = document.querySelector("#source-card-template");
const sourceTree = document.querySelector("#source-tree");
const inboxTab = document.querySelector("#inbox-tab");
const sourceTab = document.querySelector("#source-tab");
const inboxView = document.querySelector("#inbox-view");
const sourceView = document.querySelector("#source-view");
const textExtensions = new Set([".txt", ".md", ".json", ".yaml", ".yml", ".csv", ".log", ".diff", ".patch"]);
const previewImageTypes = new Set(["image/png", "image/jpeg", "image/gif"]);
let uploading = false;
let deleting = false;
let dragDepth = 0;
let maxBytes = 20 * 1024 * 1024;
let savedPaths = {host: "", dev: ""};
let itemsByName = new Map();
let currentSourcePath = "";
let sourceInitialized = false;
let markdownLoadID = 0;
const selectedNames = new Set();
const sourceTreeNodes = new Map();

chooseButton.addEventListener("click", () => fileInput.click());
inboxTab.addEventListener("click", () => selectView("inbox"));
sourceTab.addEventListener("click", () => selectView("source"));
window.addEventListener("hashchange", () => activateView(viewFromHash()));
sendTextButton.addEventListener("click", uploadTextInput);
clipboardButton.addEventListener("click", pasteClipboardText);
document.querySelector("#saved-close-button").addEventListener("click", () => {
  hideSavedItem();
});
savedHostCopyButton.addEventListener("click", () =>
  copyPath(savedHostCopyButton, savedPaths.host, "HOST パス"));
savedDevCopyButton.addEventListener("click", () =>
  copyPath(savedDevCopyButton, savedPaths.dev, "DEV パス"));
textInput.addEventListener("input", updateTextSize);
refreshButton.addEventListener("click", loadItems);
sourceRefreshButton.addEventListener("click", () => loadSource(currentSourcePath));
selectAllButton.addEventListener("click", toggleSelectAll);
deleteSelectedButton.addEventListener("click", () => {
  deleteItems([...selectedNames], `${selectedNames.size}件の共有ファイルを削除しますか？`);
});
deleteAllButton.addEventListener("click", () => {
  deleteItems([...itemsByName.keys()], `すべての共有ファイル（${itemsByName.size}件）を削除しますか？`);
});
document.querySelector("#close-preview").addEventListener("click", closePreview);
dialog.addEventListener("click", (event) => {
  if (event.target === dialog) closePreview();
});
dialog.addEventListener("close", () => {
  markdownLoadID += 1;
  previewImage.removeAttribute("src");
  previewImage.alt = "画像の拡大表示";
  previewImage.hidden = false;
  markdownView.hidden = true;
  markdownContent.replaceChildren();
});

fileInput.addEventListener("change", () => {
  uploadFiles([...fileInput.files]);
  fileInput.value = "";
});

document.addEventListener("paste", (event) => {
  if (inboxView.hidden || uploading || !event.clipboardData) return;
  const files = [...event.clipboardData.items]
    .filter((item) => item.kind === "file")
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

function viewFromHash() {
  return location.hash === "#src" ? "source" : "inbox";
}

function selectView(view) {
  const hash = view === "source" ? "#src" : "#inbox";
  if (location.hash === hash) {
    activateView(view);
  } else {
    location.hash = hash;
  }
}

async function activateView(view) {
  const sourceActive = view === "source";
  inboxView.hidden = sourceActive;
  sourceView.hidden = !sourceActive;
  chooseButton.hidden = sourceActive;
  inboxTab.classList.toggle("active", !sourceActive);
  sourceTab.classList.toggle("active", sourceActive);
  inboxTab.setAttribute("aria-selected", String(!sourceActive));
  sourceTab.setAttribute("aria-selected", String(sourceActive));
  if (sourceActive && !sourceInitialized) {
    sourceInitialized = true;
    initializeSourceTree();
    await loadSource("");
  }
}

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
    if (previewImageTypes.has(file.type)) return {kind: "image", value: file};
    const extension = fileExtension(file.name);
    if (textExtensions.has(extension) || (file.type === "text/plain" && !extension)) {
      return {kind: "textFile", value: file, extension: extension || ".txt"};
    }
    return {kind: "file", value: file};
  });
  uploadEntries(entries);
}

async function uploadEntries(entries) {
  if (uploading) {
    showStatus("現在の保存が完了してからもう一度お試しください", true);
    return 0;
  }
  if (!entries.length) {
    showStatus("ファイルまたはテキストが見つかりませんでした", true);
    return 0;
  }
  const oversized = entries.find((entry) => entrySize(entry) > maxBytes);
  if (oversized) {
    showStatus(`1ファイルの上限は${formatBytes(maxBytes)}です`, true);
    return 0;
  }

  hideSavedItem();
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
  if (succeeded) {
    const saved = results.filter((result) => result.status === "fulfilled");
    showSavedItem(saved[saved.length - 1].value, succeeded);
    await loadItems();
  }
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
        ...(entry.kind === "textFile" ? {"X-Agent-Inbox-Extension": entry.extension} : {}),
      },
      body: entry.value,
    });
  }
  if (entry.kind === "file") {
    const form = new FormData();
    form.append("file", entry.value, entry.value.name || "file.bin");
    return request("/api/files", {
      method: "POST",
      headers: {"X-Agent-Inbox": "1"},
      body: form,
    });
  }
  return Promise.reject(new Error("ファイルを保存できませんでした"));
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
  if (entry.kind === "image" || entry.kind === "textFile" || entry.kind === "file") return entry.value.size;
  return 0;
}

function fileExtension(name) {
  const match = name.toLowerCase().match(/(\.[^.]+)$/);
  return match ? match[1] : "";
}

function isMarkdownFile(name) {
  const lowerName = name.toLowerCase();
  return lowerName.endsWith(".md") || lowerName.endsWith(".markdown") || lowerName === "readme";
}

function sourceBadgeCategory(name) {
  const lowerName = name.toLowerCase();
  const extension = fileExtension(lowerName).replace(".", "");
  if (isMarkdownFile(lowerName)) return "markdown";
  if (["go", "mod", "sum"].includes(extension)) return "go";
  if (["sh", "bash", "zsh"].includes(extension)) return "shell";
  if (["js", "jsx", "ts", "tsx", "css", "scss", "html", "htm", "vue", "svelte"].includes(extension)) return "web";
  if (["yaml", "yml", "toml", "ini", "conf", "properties", "gradle", "tf", "hcl", "xml", "lock"].includes(extension)) return "config";
  if (["diff", "patch"].includes(extension)) return "change";
  if (["json", "csv", "sql", "graphql", "proto"].includes(extension)) return "data";
  if (["py", "rb", "rs", "java", "kt", "swift", "c", "h", "cc", "cpp", "hpp", "cs", "fs", "fsx", "ex", "exs", "lua", "php", "pl", "r", "dart"].includes(extension)) return "code";
  return "file";
}

function updateTextSize() {
  const size = new Blob([textInput.value]).size;
  textSize.textContent = `${formatBytes(size)} / ${formatBytes(maxBytes)}`;
  textSize.classList.toggle("over-limit", size > maxBytes);
  sendTextButton.disabled = uploading || size > maxBytes;
}

async function loadSource(path = "") {
  sourceRefreshButton.disabled = true;
  try {
    const listing = await request(`/api/source?path=${encodeURIComponent(path)}`);
    currentSourcePath = listing.path;
    populateSourceTreeNode(listing);
    selectSourceTreeNode(listing.path);
    renderSourceBreadcrumbs(listing.path);
    sourceGrid.replaceChildren();
    sourceEmptyState.textContent = "このディレクトリは空です。";
    sourceEmptyState.hidden = listing.entries.length !== 0;
    for (const item of listing.entries) sourceGrid.append(createSourceCard(item));
  } catch (error) {
    sourceGrid.replaceChildren();
    sourceEmptyState.textContent = "src内のファイル一覧を読み込めませんでした。";
    sourceEmptyState.hidden = false;
    showStatus(error.message, true);
  } finally {
    sourceRefreshButton.disabled = false;
  }
}

function initializeSourceTree() {
  sourceTree.replaceChildren();
  sourceTreeNodes.clear();
  sourceTree.append(createSourceTreeNode("", "src", 1));
}

function createSourceTreeNode(path, name, level) {
  const item = document.createElement("li");
  item.className = "source-tree-node";
  item.dataset.path = path;
  item.setAttribute("role", "treeitem");
  item.setAttribute("aria-level", String(level));
  item.setAttribute("aria-expanded", "false");

  const row = document.createElement("div");
  row.className = "source-tree-row";
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.className = "source-tree-toggle";
  toggle.textContent = "›";
  toggle.setAttribute("aria-label", `${name} を展開`);
  const nameButton = document.createElement("button");
  nameButton.type = "button";
  nameButton.className = "source-tree-name";
  nameButton.textContent = name;
  nameButton.addEventListener("click", () => loadSource(path));
  toggle.addEventListener("click", () => toggleSourceTreeNode(path));
  row.append(toggle, nameButton);

  const children = document.createElement("ul");
  children.className = "source-tree-children";
  children.setAttribute("role", "group");
  children.hidden = true;
  item.append(row, children);
  sourceTreeNodes.set(path, {item, row, toggle, children, level, loaded: false});
  return item;
}

async function toggleSourceTreeNode(path) {
  const node = sourceTreeNodes.get(path);
  if (!node || node.toggle.disabled) return;
  if (!node.loaded) {
    node.toggle.disabled = true;
    try {
      const listing = await request(`/api/source?path=${encodeURIComponent(path)}`);
      populateSourceTreeNode(listing);
    } catch (error) {
      node.toggle.disabled = false;
      showStatus(error.message, true);
    }
    return;
  }
  setSourceTreeExpanded(node, node.children.hidden);
}

function populateSourceTreeNode(listing) {
  const node = sourceTreeNodes.get(listing.path);
  if (!node) return;
  const directories = listing.entries.filter((entry) => entry.kind === "directory");
  const wantedPaths = new Set(directories.map((entry) => entry.path));
  for (const child of [...node.children.children]) {
    if (!wantedPaths.has(child.dataset.path)) removeSourceTreeBranch(child.dataset.path);
  }

  const fragment = document.createDocumentFragment();
  for (const directory of directories) {
    let childNode = sourceTreeNodes.get(directory.path);
    if (!childNode) {
      createSourceTreeNode(directory.path, directory.name, node.level + 1);
      childNode = sourceTreeNodes.get(directory.path);
    }
    fragment.append(childNode.item);
  }
  node.children.replaceChildren(fragment);
  node.loaded = true;
  node.toggle.disabled = directories.length === 0;
  setSourceTreeExpanded(node, directories.length !== 0);
}

function removeSourceTreeBranch(path) {
  for (const candidate of [...sourceTreeNodes.keys()]) {
    if (candidate === path || candidate.startsWith(`${path}/`)) {
      sourceTreeNodes.delete(candidate);
    }
  }
}

function setSourceTreeExpanded(node, expanded) {
  const canExpand = !node.toggle.disabled;
  node.children.hidden = !expanded || !canExpand;
  node.item.setAttribute("aria-expanded", String(expanded && canExpand));
  node.toggle.textContent = expanded && canExpand ? "⌄" : "›";
}

function selectSourceTreeNode(path) {
  for (const node of sourceTreeNodes.values()) {
    const selected = node.item.dataset.path === path;
    node.row.classList.toggle("selected", selected);
    node.item.setAttribute("aria-selected", String(selected));
  }
}

function renderSourceBreadcrumbs(path) {
  sourceBreadcrumbs.replaceChildren();
  const rootButton = document.createElement("button");
  rootButton.type = "button";
  rootButton.className = "source-crumb";
  rootButton.textContent = "src";
  rootButton.disabled = path === "";
  rootButton.addEventListener("click", () => loadSource(""));
  sourceBreadcrumbs.append(rootButton);

  let accumulated = "";
  for (const part of path.split("/").filter(Boolean)) {
    const separator = document.createElement("span");
    separator.textContent = "/";
    separator.setAttribute("aria-hidden", "true");
    sourceBreadcrumbs.append(separator);
    accumulated = accumulated ? `${accumulated}/${part}` : part;
    const target = accumulated;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "source-crumb";
    button.textContent = part;
    button.disabled = target === path;
    button.addEventListener("click", () => loadSource(target));
    sourceBreadcrumbs.append(button);
  }
}

function createSourceCard(item) {
  const card = sourceTemplate.content.firstElementChild.cloneNode(true);
  const preview = card.querySelector(".source-preview");
  const image = card.querySelector("img");
  const icon = card.querySelector(".source-file-icon");
  const extensionLabel = card.querySelector(".source-extension");
  const openButton = card.querySelector(".source-open-button");
  const copyButton = card.querySelector(".source-copy-button");
  const nameButton = card.querySelector(".source-name");

  nameButton.textContent = item.name;
  card.querySelector(".source-path").textContent = item.hostPath;
  const kindLabels = {directory: "DIRECTORY", image: "IMAGE", text: "TEXT", file: "FILE"};
  card.querySelector(".source-metadata").textContent = item.kind === "directory"
    ? `DIRECTORY · ${new Date(item.time).toLocaleString()}`
    : `${kindLabels[item.kind] || "FILE"} · ${formatBytes(item.size)} · ${new Date(item.time).toLocaleString()}`;
  copyButton.addEventListener("click", () => copyPath(copyButton, item.hostPath, "HOST パス"));

  const activate = () => {
    if (item.kind === "directory") {
      loadSource(item.path);
    } else if (item.kind === "image") {
      previewImage.src = item.url;
      previewImage.alt = item.name;
      dialog.showModal();
    } else if (item.kind === "text" && isMarkdownFile(item.name)) {
      openMarkdown(item);
    } else if (item.kind === "text") {
      window.open(item.url, "_blank", "noopener");
    } else {
      window.location.assign(item.url);
    }
  };
  nameButton.addEventListener("click", activate);
  preview.addEventListener("click", activate);

  if (item.kind === "image") {
    image.hidden = false;
    image.src = item.url;
    image.alt = item.name;
    icon.hidden = true;
    preview.setAttribute("aria-label", `${item.name} を拡大`);
  } else {
    const extension = fileExtension(item.name).replace(".", "").toUpperCase();
    extensionLabel.textContent = item.kind === "directory" ? "" : isMarkdownFile(item.name) ? "MD" : extension.slice(0, 4);
    extensionLabel.dataset.category = sourceBadgeCategory(item.name);
    preview.setAttribute("aria-label", `${item.name} へ${item.kind === "directory" ? "移動" : "表示"}`);
  }

  if (item.kind === "directory") {
    card.classList.add("directory");
    openButton.hidden = true;
  } else {
    openButton.href = item.url;
    if (item.kind === "file") {
      openButton.textContent = "ダウンロード";
      openButton.removeAttribute("target");
    } else if (item.kind === "image") {
      openButton.textContent = "原寸表示";
    } else if (isMarkdownFile(item.name)) {
      openButton.textContent = "読む";
      openButton.addEventListener("click", (event) => {
        event.preventDefault();
        openMarkdown(item);
      });
    } else {
      openButton.textContent = "表示";
    }
  }
  return card;
}

async function openMarkdown(item) {
  const loadID = ++markdownLoadID;
  previewImage.hidden = true;
  markdownView.hidden = false;
  markdownTitle.textContent = item.name;
  markdownRawLink.href = item.url;
  markdownContent.replaceChildren();
  const loading = document.createElement("p");
  loading.className = "markdown-loading";
  loading.textContent = "読み込み中…";
  markdownContent.append(loading);
  if (!dialog.open) dialog.showModal();

  try {
    const response = await fetch(`/api/source/markdown?path=${encodeURIComponent(item.path)}`);
    if (!response.ok) throw new Error(response.status === 413 ? "too-large" : `HTTP ${response.status}`);
    const rendered = await response.text();
    if (loadID !== markdownLoadID) return;
    renderMarkdownPreview(rendered, markdownContent, item.url);
  } catch (cause) {
    if (loadID !== markdownLoadID) return;
    markdownContent.replaceChildren();
    const error = document.createElement("p");
    error.className = "markdown-loading error";
    error.textContent = cause.message === "too-large"
      ? "500 KiBを超えるMarkdownはプレビューできません。Rawで表示してください。"
      : "Markdownを読み込めませんでした。";
    markdownContent.append(error);
  }
}

function renderMarkdownPreview(rendered, container, sourceURL) {
  container.innerHTML = rendered;
  const baseURL = new URL(sourceURL, window.location.href);
  for (const link of container.querySelectorAll("a[href]")) {
    normalizeMarkdownURL(link, "href", baseURL);
    link.target = "_blank";
    link.rel = "noopener noreferrer";
  }
  for (const image of container.querySelectorAll("img[src]")) {
    normalizeMarkdownURL(image, "src", baseURL);
    image.loading = "lazy";
  }
}

function normalizeMarkdownURL(element, attribute, baseURL) {
  try {
    const url = new URL(element.getAttribute(attribute), baseURL);
    if (["http:", "https:"].includes(url.protocol)) {
      element.setAttribute(attribute, url.href);
      return;
    }
  } catch {
    // Remove unsafe or malformed destinations below.
  }
  element.removeAttribute(attribute);
}

async function loadItems() {
  try {
    const items = await request("/api/items");
    itemsByName = new Map(items.map((item) => [item.name, item]));
    for (const name of selectedNames) {
      if (!itemsByName.has(name)) selectedNames.delete(name);
    }
    gallery.replaceChildren();
    emptyState.textContent = "まだ共有ファイルがありません。";
    emptyState.hidden = items.length !== 0;
    for (const item of items) gallery.append(createCard(item));
    updateSelectionControls();
    return true;
  } catch (error) {
    itemsByName.clear();
    selectedNames.clear();
    gallery.replaceChildren();
    emptyState.textContent = "共有ファイルの読み込みに失敗しました。";
    emptyState.hidden = false;
    updateSelectionControls();
    showStatus(error.message, true);
    return false;
  }
}

function createCard(item) {
  const card = template.content.firstElementChild.cloneNode(true);
  card.dataset.name = item.name;
  const checkbox = card.querySelector(".select-checkbox");
  checkbox.checked = selectedNames.has(item.name);
  checkbox.setAttribute("aria-label", `${item.name} を選択`);
  checkbox.addEventListener("change", () => {
    if (checkbox.checked) {
      selectedNames.add(item.name);
    } else {
      selectedNames.delete(item.name);
    }
    card.classList.toggle("selected", checkbox.checked);
    updateSelectionControls();
  });
  card.classList.toggle("selected", checkbox.checked);
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
    textPreview.textContent = item.kind === "text" ? (item.snippet || "TXT") : (fileExtension(item.name).slice(1).toUpperCase() || "FILE");
    textPreview.id = `snippet-${item.name}`;
    card.querySelector(".preview").setAttribute("aria-label", `${item.name} を${item.kind === "file" ? "ダウンロード" : "開く"}`);
    card.querySelector(".preview").setAttribute("aria-describedby", textPreview.id);
  }
  card.querySelector(".host-path").textContent = `HOST ${item.hostPath}`;
  card.querySelector(".dev-path").textContent = `DEV  ${item.containerPath}`;
  const kindLabels = {image: "IMAGE", text: "TEXT", file: "FILE"};
  card.querySelector(".metadata").textContent =
    `${kindLabels[item.kind] || "FILE"} · ${formatBytes(item.size)} · ${new Date(item.time).toLocaleString()}`;

  card.querySelector(".preview").addEventListener("click", () => {
    if (item.kind !== "image") {
      window.open(item.url, "_blank", "noopener");
      return;
    }
    previewImage.src = item.url;
    dialog.showModal();
  });
  const hostCopyButton = card.querySelector(".host-copy-button");
  const devCopyButton = card.querySelector(".dev-copy-button");
  hostCopyButton.addEventListener("click", () =>
    copyPath(hostCopyButton, item.hostPath, "HOST パス"));
  devCopyButton.addEventListener("click", () =>
    copyPath(devCopyButton, item.containerPath, "DEV パス"));
  card.querySelector(".delete-button").addEventListener("click", async () => {
    if (!confirm(`${item.name} を削除しますか？`)) return;
    try {
      await request(`/api/items/${encodeURIComponent(item.name)}`, {
        method: "DELETE",
        headers: {"X-Agent-Inbox": "1"},
      });
      card.remove();
      itemsByName.delete(item.name);
      selectedNames.delete(item.name);
      emptyState.hidden = gallery.children.length !== 0;
      if (savedPaths.host === item.hostPath || savedPaths.dev === item.containerPath) {
        hideSavedItem();
      }
      updateSelectionControls();
      showStatus("共有ファイルを削除しました");
    } catch (error) {
      showStatus(error.message, true);
    }
  });
  return card;
}

function toggleSelectAll() {
  const select = selectedNames.size !== itemsByName.size;
  selectedNames.clear();
  if (select) {
    for (const name of itemsByName.keys()) selectedNames.add(name);
  }
  for (const card of gallery.children) {
    const checked = selectedNames.has(card.dataset.name);
    card.querySelector(".select-checkbox").checked = checked;
    card.classList.toggle("selected", checked);
  }
  updateSelectionControls();
}

function updateSelectionControls() {
  const total = itemsByName.size;
  const selected = selectedNames.size;
  selectionStatus.textContent = `${selected}件選択中`;
  selectionStatus.hidden = total === 0;
  selectAllButton.textContent = total > 0 && selected === total ? "選択を解除" : "すべて選択";
  selectAllButton.disabled = deleting || total === 0;
  deleteSelectedButton.disabled = deleting || selected === 0;
  deleteAllButton.disabled = deleting || total === 0;
  refreshButton.disabled = deleting;
  for (const card of gallery.children) {
    card.querySelector(".select-checkbox").disabled = deleting;
    card.querySelector(".delete-button").disabled = deleting;
  }
}

async function deleteItems(names, confirmation) {
  if (deleting || names.length === 0 || !confirm(confirmation)) return;
  deleting = true;
  updateSelectionControls();
  showStatus(`${names.length}件の共有ファイルを削除しています…`);

  const results = await Promise.allSettled(names.map((name) =>
    request(`/api/items/${encodeURIComponent(name)}`, {
      method: "DELETE",
      headers: {"X-Agent-Inbox": "1"},
    })));
  const failedNames = names.filter((_, index) => results[index].status === "rejected");
  const deletedNames = names.filter((_, index) => results[index].status === "fulfilled");

  for (const name of deletedNames) {
    const item = itemsByName.get(name);
    if (item && (savedPaths.host === item.hostPath || savedPaths.dev === item.containerPath)) {
      hideSavedItem();
    }
    itemsByName.delete(name);
    selectedNames.delete(name);
    gallery.querySelector(`[data-name="${CSS.escape(name)}"]`)?.remove();
  }

  if (failedNames.length) {
    for (const name of failedNames) selectedNames.add(name);
    const synchronized = await loadItems();
    deleting = false;
    updateSelectionControls();
    if (synchronized) {
      showStatus(`${deletedNames.length}件を削除、${failedNames.length}件の削除に失敗しました`, true);
    } else {
      showStatus(`${deletedNames.length}件を削除しましたが、最新の一覧を読み込めませんでした`, true);
    }
  } else {
    deleting = false;
    emptyState.hidden = itemsByName.size !== 0;
    updateSelectionControls();
    showStatus(`${deletedNames.length}件の共有ファイルを削除しました`);
  }
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
  textarea.style.top = "0";
  textarea.style.left = "-9999px";
  textarea.style.opacity = "0";
  document.body.append(textarea);
  textarea.select();
  textarea.setSelectionRange(0, textarea.value.length);
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("clipboard copy failed");
}

async function copyPath(button, path, label) {
  try {
    await copyText(path);
    button.textContent = "コピーしました";
    setTimeout(() => { button.textContent = label; }, 1200);
  } catch {
    window.prompt("次のパスをコピーしてください", path);
  }
}

function showStatus(message, isError = false) {
  statusElement.textContent = message;
  statusElement.classList.toggle("error", isError);
  statusElement.classList.add("visible");
  clearTimeout(showStatus.timeout);
  showStatus.timeout = setTimeout(() => statusElement.classList.remove("visible"), 4500);
}

function showSavedItem(item, count) {
  savedPaths = {host: item.hostPath, dev: item.containerPath};
  savedMessage.textContent =
    count > 1 ? `${count}件保存しました（最後のファイル）` : "保存しました";
  savedHostPath.textContent = `HOST ${item.hostPath}`;
  savedDevPath.textContent = `DEV  ${item.containerPath}`;
  savedHostCopyButton.textContent = "HOST パス";
  savedDevCopyButton.textContent = "DEV パス";
  savedItem.hidden = false;
}

function hideSavedItem() {
  savedItem.hidden = true;
  savedPaths = {host: "", dev: ""};
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
    sourceRootElement.textContent = config.sourceRoot;
  } catch (error) {
    showStatus(error.message, true);
  }
  updateTextSize();
  await loadItems();
  await activateView(viewFromHash());
}

initialize();
