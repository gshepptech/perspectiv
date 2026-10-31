// Applies the reader's Appearance choices (src/lib/appearance.ts) before the
// first paint, so a page never flashes the other theme. Loaded as a file, not
// inline, so a script-src 'self' policy allows it. Keep the keys, values and
// colours in step with src/lib/appearance.ts.
(function () {
  var root = document.documentElement;
  try {
    var theme = localStorage.getItem("perspectiv.theme");
    var text = localStorage.getItem("perspectiv.text");
    if (theme === "light" || theme === "dark") {
      root.dataset.theme = theme;
      var colour = theme === "dark" ? "#161514" : "#F7F5F0";
      document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
        m.setAttribute("content", colour);
      });
    }
    if (text === "large" || text === "larger") root.dataset.text = text;
  } catch (e) {
    // A blocked store means the defaults: the device's theme, standard text.
  }
})();
