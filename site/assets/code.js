/* Shared copy controls for homepage examples and documentation. */
(function () {
  "use strict";

  document.querySelectorAll("[data-copy]").forEach(function (button) {
    button.hidden = false;
    var label = button.textContent;
    var reset;
    button.addEventListener("click", function () {
      // Resolve on click: the language tabs update the target.
      var target = document.querySelector(button.getAttribute("data-copy"));
      if (!target) return;
      var text = target.textContent.trim();
      function done(ok) {
        button.textContent = ok ? "Copied" : "Select & copy";
        button.setAttribute("data-copied", ok ? "1" : "0");
        clearTimeout(reset);
        reset = setTimeout(function () {
          button.textContent = label;
          button.removeAttribute("data-copied");
        }, 1600);
      }
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(
          function () { done(true); },
          function () { selectText(); }
        );
      } else selectText();
      function selectText() {
        var range = document.createRange();
        range.selectNodeContents(target);
        var selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        done(false);
      }
    });
  });

})();
