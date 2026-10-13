/* Illustrative state walkthrough only. The linked browser-editor example runs
 * the real runtime. No file contents leave this tab. */
(function () {
  "use strict";
  var demo = document.querySelector("[data-demo]");
  if (!demo) return;
  var byId = function (id) { return document.getElementById("demo-" + id); };
  var editor = byId("editor");
  var initial = editor.value;
  var stages = [initial];
  var cursor = 0;
  var remote = initial;
  var epoch = 0;
  var maxBytes = 16 * 1024;
  var bytes = function (text) { return new TextEncoder().encode(text).length; };
  var pending = function () { return stages[cursor] !== remote; };
  function render(message) {
    var dirty = editor.value !== stages[cursor];
    byId("save").disabled = !dirty;
    byId("undo").disabled = cursor === 0;
    byId("redo").disabled = cursor === stages.length - 1;
    byId("sync").disabled = dirty || byId("offline").checked || !pending();
    byId("pending").textContent = pending() ? "1" : "0";
    byId("remote-content").textContent = remote === null ? "No remote object yet." : remote;
    byId("history").textContent = stages.length + " retained stage" + (stages.length === 1 ? "" : "s") +
      " · selected " + (cursor + 1) + (dirty ? " · unsaved edit" : " · saved in this tab");
    if (message) byId("message").textContent = message;
  }
  editor.addEventListener("input", function () {
    render("Save a stage to keep this edit in the walkthrough history.");
  });
  byId("save").addEventListener("click", function () {
    if (bytes(editor.value) > maxBytes) {
      render("This walkthrough accepts up to 16 KiB of text. Shorten the edit before saving.");
      return;
    }
    stages = stages.slice(0, cursor + 1);
    stages.push(editor.value);
    if (stages.length > 50) stages.shift();
    cursor = stages.length - 1;
    render("Stage saved in this tab. The simulated remote has not changed.");
  });
  ["undo", "redo"].forEach(function (direction) {
    byId(direction).addEventListener("click", function () {
      cursor += direction === "undo" ? -1 : 1;
      editor.value = stages[cursor];
      render("Selected a retained stage. Sync publishes this selection when you choose.");
    });
  });
  byId("offline").addEventListener("change", function () {
    render(this.checked ? "Simulated offline mode. Keep editing; sync is unavailable." :
      "Reconnected. The simulated remote stays unchanged until you sync.");
  });
  byId("sync").addEventListener("click", function () {
    remote = stages[cursor];
    render("Selected stage copied to the simulated remote. No network request was made.");
  });
  byId("upload").addEventListener("change", async function () {
    var file = this.files[0];
    if (!file) return;
    var ticket = ++epoch;
    if (file.size > maxBytes) {
      render("Choose a text file no larger than 16 KiB.");
      this.value = "";
      return;
    }
    try {
      var data = new Uint8Array(await file.arrayBuffer());
      var content = new TextDecoder("utf-8", { fatal: true }).decode(data);
      if (ticket !== epoch) return;
      if (content.includes("\u0000")) throw new Error("binary file");
      stages = [content];
      cursor = 0;
      remote = null;
      editor.value = content;
      byId("filename").textContent = file.name;
      render("Opened locally in this tab. The simulated remote has no copy until you sync.");
    } catch (error) {
      if (ticket === epoch) render("Choose a UTF-8 text file. The current working copy was kept.");
    }
    if (ticket === epoch) this.value = "";
  });
  byId("reset").addEventListener("click", function () {
    epoch += 1;
    stages = [initial]; cursor = 0; remote = initial;
    editor.value = initial;
    byId("filename").textContent = "notes.md";
    byId("offline").checked = false;
    byId("upload").value = "";
    render("Reset. The initial stage matches the simulated remote.");
  });
  render();
  demo.hidden = false;
})();
