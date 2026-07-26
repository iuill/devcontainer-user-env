const gallery = document.querySelector("#gallery");
const emptyState = document.querySelector("#empty-state");
const status = document.querySelector("#status");
const dropZone = document.querySelector("#drop-zone");
const fileInput = document.querySelector("#file-input");
const template = document.querySelector("#image-card-template");
const dialog = document.querySelector("#preview-dialog");
const previewImage = document.querySelector("#preview-image");

document.querySelector("#choose-button").addEventListener("click", () => fileInput.click());
document.querySelector("#refresh-button").addEventListener("click", loadImages);
document.querySelector("#close-preview").addEventListener("click", () => dialog.close());
dialog.addEventListener("click", (event) => {
  if (event.target === dialog) dialog.close();
});

fileInput.addEventListener("change", () => {
  uploadFiles([...fileInput.files]);
  fileInput.value = "";
});

document.addEventListener("paste", (event) => {
  const files = [...event.clipboardData.items]
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter(Boolean);
  if (files.length) {
    event.preventDefault();
    uploadFiles(files);
  }
});

for (const eventName of ["dragenter", "dragover"]) {
  dropZone.addEventListener(eventName, (event) => {
    event.preventDefault();
    dropZone.classList.add("dragging");
  });
}
for (const eventName of ["dragleave", "drop"]) {
  dropZone.addEventListener(eventName, (event) => {
    event.preventDefault();
    dropZone.classList.remove("dragging");
  });
}
dropZone.addEventListener("drop", (event) => {
  uploadFiles([...event.dataTransfer.files].filter((file) => file.type.startsWith("image/")));
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
  if (!files.length) {
    showStatus("画像が見つかりませんでした", true);
    return;
  }
  showStatus(`${files.length}件の画像を保存しています…`);
  try {
    for (const file of files) {
      const form = new FormData();
      form.append("image", file, file.name || "clipboard.png");
      await request("/api/images", {
        method: "POST",
        headers: {"X-Screenshot-Web": "1"},
        body: form,
      });
    }
    showStatus(`${files.length}件の画像を保存しました`);
    await loadImages();
  } catch (error) {
    showStatus(error.message, true);
  }
}

async function loadImages() {
  try {
    const images = await request("/api/images");
    gallery.replaceChildren();
    emptyState.hidden = images.length !== 0;
    for (const image of images) gallery.append(createCard(image));
  } catch (error) {
    showStatus(error.message, true);
  }
}

function createCard(image) {
  const card = template.content.firstElementChild.cloneNode(true);
  const img = card.querySelector("img");
  img.src = image.url;
  img.alt = image.name;
  card.querySelector(".path").textContent = image.path;
  card.querySelector(".metadata").textContent =
    `${formatBytes(image.size)} · ${new Date(image.time).toLocaleString()}`;

  card.querySelector(".preview").addEventListener("click", () => {
    previewImage.src = image.url;
    dialog.showModal();
  });
  card.querySelector(".copy-button").addEventListener("click", async (event) => {
    try {
      await navigator.clipboard.writeText(image.path);
      const button = event.currentTarget;
      button.textContent = "コピーしました";
      setTimeout(() => { button.textContent = "パスをコピー"; }, 1200);
    } catch {
      showStatus(`コピーできませんでした: ${image.path}`, true);
    }
  });
  card.querySelector(".delete-button").addEventListener("click", async () => {
    if (!confirm(`${image.name} を削除しますか？`)) return;
    try {
      await request(`/api/images/${encodeURIComponent(image.name)}`, {
        method: "DELETE",
        headers: {"X-Screenshot-Web": "1"},
      });
      card.remove();
      emptyState.hidden = gallery.children.length !== 0;
      showStatus("画像を削除しました");
    } catch (error) {
      showStatus(error.message, true);
    }
  });
  return card;
}

function showStatus(message, isError = false) {
  status.textContent = message;
  status.classList.toggle("error", isError);
  status.classList.add("visible");
  clearTimeout(showStatus.timeout);
  showStatus.timeout = setTimeout(() => status.classList.remove("visible"), 3500);
}

function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

loadImages();
