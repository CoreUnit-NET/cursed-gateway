function decode(part) {
  try {
    return decodeURIComponent(part);
  } catch {
    return part;
  }
}

export function parseHash() {
  const parts = (location.hash || "#/")
    .replace(/^#/, "")
    .split("/")
    .filter(Boolean)
    .map(decode);

  if (parts[0] === "accounts") {
    if (parts[1] === "add") return { tab: "accounts", mode: "add" };
    if (parts[1] === "import") return { tab: "accounts", mode: "import" };
    if (parts[1]) return { tab: "accounts", mode: "detail", id: parts[1] };
    return { tab: "accounts", mode: "pool" };
  }
  if (parts[0] === "login") {
    if (parts[1] === "start") return { tab: "login", mode: "start" };
    if (parts[1]) return { tab: "login", mode: "detail", id: parts[1] };
    return { tab: "login", mode: "attempts" };
  }
  return { tab: "overview", mode: "home" };
}

export function routeKey(route) {
  return [route.tab, route.mode, route.id || ""].join("/");
}

export function watchingLogins(route) {
  return (
    route.tab === "login" &&
    (route.mode === "attempts" || route.mode === "detail")
  );
}

export function accountHref(id) {
  return "#/accounts/" + encodeURIComponent(id);
}

export function loginHref(id) {
  return "#/login/" + encodeURIComponent(id);
}
