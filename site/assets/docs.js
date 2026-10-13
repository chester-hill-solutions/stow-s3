const navigation = document.querySelector(".docs-sidebar details");
const narrowScreen = window.matchMedia("(max-width: 760px)");
function updateNavigation() {
  if (navigation) navigation.open = !narrowScreen.matches;
}
updateNavigation();
narrowScreen.addEventListener("change", updateNavigation);
