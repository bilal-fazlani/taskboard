// The history guard for HTML documents: what it does and why is in
// documentguard.go, which embeds this file.
(function () {
  "use strict";

  var replaceState = History.prototype.replaceState;
  Object.defineProperty(History.prototype, "pushState", {
    configurable: true,
    writable: true,
    value: function pushState() {
      return replaceState.apply(this, arguments);
    },
  });

  if (typeof Navigation === "function" && typeof Navigation.prototype.navigate === "function") {
    var navigate = Navigation.prototype.navigate;
    Object.defineProperty(Navigation.prototype, "navigate", {
      configurable: true,
      writable: true,
      value: function (url, options) {
        return navigate.call(this, url, Object.assign({}, options, { history: "replace" }));
      },
    });
  }

  // The URL a clicked link element leads to, or "" when it is not a link.
  // An SVG <a> keeps its href (or xlink:href) in an SVGAnimatedString.
  function linkURL(el) {
    if (el.localName !== "a" && el.localName !== "area") return "";
    if (typeof el.href === "string") return el.hasAttribute("href") ? el.href : "";
    var raw = el.href && el.href.baseVal;
    return raw ? new URL(raw, document.baseURI).href : "";
  }

  function targetOf(el) {
    var target = el.getAttribute("target");
    if (target === null) {
      var base = document.querySelector("base[target]");
      target = base ? base.getAttribute("target") : "";
    }
    return target.trim().toLowerCase();
  }

  // Takes the click over when it would follow a link in this page.
  function follow(event) {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    var path = event.composedPath();
    for (var i = 0; i < path.length; i++) {
      var el = path[i];
      if (!el || el.nodeType !== 1) continue;
      var url = linkURL(el);
      if (!url) continue;
      if (el.hasAttribute("download")) return;
      var target = targetOf(el);
      if (target && target !== "_self") return;
      var protocol = new URL(url).protocol;
      if (protocol !== "http:" && protocol !== "https:") return;
      event.preventDefault();
      location.replace(url);
      return;
    }
  }

  // The page's own listeners decide first, so a click the page cancels is
  // left alone. This capture listener runs before them all and adds a
  // bubbling listener for the same click, which goes last on window, after
  // every listener the page added, even ones added after this guard. It is
  // tied to its own event: when the page stops the click before it reaches
  // window, the next click removes the leftover unused.
  var pending = null;
  addEventListener(
    "click",
    function (event) {
      if (pending) removeEventListener("click", pending);
      var decide = function (e) {
        removeEventListener("click", decide);
        if (pending === decide) pending = null;
        if (e === event) follow(e);
      };
      pending = decide;
      addEventListener("click", decide);
    },
    true,
  );
})();
