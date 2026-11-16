/** Applies a saved theme before stylesheets paint to avoid a theme flash. */
(() => {
  try {
    const savedTheme = window.localStorage.getItem("war-room.theme");
    if (savedTheme === "dark" || savedTheme === "light") {
      document.documentElement.dataset.theme = savedTheme;
    }
  } catch {
    // The CSS system preference remains available when browser storage is blocked.
  }
})();
