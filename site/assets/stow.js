/* Progressive enhancements: language examples and demo links. */
(function () {
  "use strict";

  document.querySelectorAll("[data-examples]").forEach(function (examples) {
    var bar = examples.querySelector(".example-tabs");
    var tabs = Array.from(examples.querySelectorAll('[role="tab"]'));
    var copy = bar.querySelector("[data-copy]");
    function select(index, focus) {
      tabs.forEach(function (tab, position) {
        var selected = position === index;
        var panel = document.getElementById(tab.getAttribute("aria-controls"));
        tab.setAttribute("aria-selected", String(selected));
        tab.tabIndex = selected ? 0 : -1;
        panel.hidden = !selected;
        panel.setAttribute("role", "tabpanel");
        panel.setAttribute("aria-labelledby", tab.id);
        if (selected) {
          var code = panel.querySelector("code");
          copy.setAttribute("data-copy", "#" + code.id);
          copy.setAttribute("aria-label", "Copy " + tab.textContent + " example");
          examples.querySelector("[data-example-file]").textContent = code.getAttribute("data-file");
          examples.querySelector("[data-example-language]").textContent = tab.textContent;
        }
      });
      if (focus) tabs[index].focus();
    }
    tabs.forEach(function (tab, index) {
      tab.addEventListener("click", function () { select(index, false); });
      tab.addEventListener("keydown", function (event) {
        var next;
        if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
        if (event.key === "ArrowLeft") next = (index + tabs.length - 1) % tabs.length;
        if (event.key === "Home") next = 0;
        if (event.key === "End") next = tabs.length - 1;
        if (next !== undefined) {
          event.preventDefault();
          select(next, true);
        }
      });
    });
    examples.setAttribute("data-enhanced", "");
    select(0, false);
    bar.hidden = false;
    examples.querySelector(".code-editor-heading").hidden = false;
  });

  function revealFragment(fragment) {
    var id;
    var hash = typeof fragment === "string" ? fragment : window.location.hash;
    try { id = decodeURIComponent(hash.slice(1)); }
    catch (error) { return; }
    var target = document.getElementById(id);
    if (!target) return;
    var disclosure = target.closest("details");
    if (disclosure && !disclosure.open) {
      disclosure.open = true;
      target.scrollIntoView();
    }
  }
  revealFragment();
  window.addEventListener("hashchange", revealFragment);
  document.querySelectorAll('a[href^="#"]').forEach(function (link) {
    link.addEventListener("click", function (event) {
      if (event.button || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      revealFragment(link.getAttribute("href"));
    });
  });
})();
