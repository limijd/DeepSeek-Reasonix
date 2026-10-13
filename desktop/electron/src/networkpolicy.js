"use strict";

function allowedHost(raw) {
  const host = String(raw).toLowerCase().replace(/\.$/, "");
  return host !== "reasonix.io" && !host.endsWith(".reasonix.io") && host !== "api.typesafe.ai";
}

function networkAllowed(raw) {
  let url;
  try { url = new URL(String(raw)); } catch { return false; }
  if (["about:", "data:", "blob:", "file:", "devtools:", "chrome-devtools:"].includes(url.protocol)) return true;
  return ["http:", "https:", "ws:", "wss:"].includes(url.protocol) && allowedHost(url.hostname);
}

function installNetworkPolicy(ses) {
  ses.webRequest.onBeforeRequest((details, done) => done({ cancel: !networkAllowed(details.url) }));
}

module.exports = { allowedHost, networkAllowed, installNetworkPolicy };
