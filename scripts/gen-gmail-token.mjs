/**
 * One-time helper to generate a Google OAuth2 refresh token for the Go backend.
 *
 * Usage:
 *   node scripts/gen-gmail-token.mjs
 *
 * Required env vars, from process env, .env.local, or .env:
 *   GOOGLE_CLIENT_ID
 *   GOOGLE_CLIENT_SECRET
 *
 * Add this redirect URI to the OAuth client in Google Cloud Console:
 *   http://localhost:9876/callback
 */

import { createServer } from "node:http";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const DEFAULT_PORT = 9876;
const SCOPES = [
  "https://www.googleapis.com/auth/calendar.readonly",
  "https://www.googleapis.com/auth/calendar.events",
  "https://www.googleapis.com/auth/gmail.modify",
  "https://www.googleapis.com/auth/gmail.send",
  "https://www.googleapis.com/auth/gmail.readonly",
];

function parseEnvFile(path) {
  if (!existsSync(path)) return {};

  const env = {};
  for (const rawLine of readFileSync(path, "utf8").split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;

    const eq = line.indexOf("=");
    if (eq <= 0) continue;

    const key = line.slice(0, eq).trim();
    let value = line.slice(eq + 1).trim();
    value = value.replace(/^['"]|['"]$/g, "");
    env[key] = value;
  }

  return env;
}

function loadEnv() {
  return {
    ...parseEnvFile(resolve(process.cwd(), ".env.local")),
    ...parseEnvFile(resolve(process.cwd(), ".env")),
    ...process.env,
  };
}

function buildAuthUrl(clientID, redirectURI) {
  const params = new URLSearchParams({
    client_id: clientID,
    redirect_uri: redirectURI,
    response_type: "code",
    access_type: "offline",
    prompt: "consent",
    scope: SCOPES.join(" "),
  });

  return `https://accounts.google.com/o/oauth2/v2/auth?${params.toString()}`;
}

async function exchangeCode({ clientID, clientSecret, redirectURI, code }) {
  const body = new URLSearchParams({
    client_id: clientID,
    client_secret: clientSecret,
    redirect_uri: redirectURI,
    code,
    grant_type: "authorization_code",
  });

  const response = await fetch("https://oauth2.googleapis.com/token", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });

  const text = await response.text();
  const data = JSON.parse(text);

  if (!response.ok) {
    throw new Error(`Token exchange failed (${response.status}): ${text}`);
  }

  return data;
}

const env = loadEnv();
const clientID = env.GOOGLE_CLIENT_ID;
const clientSecret = env.GOOGLE_CLIENT_SECRET;
const port = Number(env.GOOGLE_OAUTH_PORT || DEFAULT_PORT);
const redirectURI = `http://localhost:${port}/callback`;

if (!clientID || !clientSecret) {
  console.error("Missing GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET.");
  console.error("Put them in .env, .env.local, or your shell environment.");
  process.exit(1);
}

const authUrl = buildAuthUrl(clientID, redirectURI);

console.log("");
console.log("Add this redirect URI to your Google OAuth client:");
console.log(`  ${redirectURI}`);
console.log("");
console.log("Open this URL in your browser:");
console.log("");
console.log(authUrl);
console.log("");
console.log("Waiting for Google callback...");
console.log("");

const server = createServer(async (req, res) => {
  const url = new URL(req.url || "/", redirectURI);

  if (url.pathname !== "/callback") {
    res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
    res.end("Not found");
    return;
  }

  const code = url.searchParams.get("code");
  if (!code) {
    res.writeHead(400, { "Content-Type": "text/html; charset=utf-8" });
    res.end("<h1>No OAuth code received</h1><p>Try again.</p>");
    return;
  }

  try {
    const tokens = await exchangeCode({ clientID, clientSecret, redirectURI, code });

    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    res.end("<h1>Token received</h1><p>You can close this tab and return to the terminal.</p>");

    console.log("============================================================");
    console.log("Google OAuth tokens received");
    console.log("============================================================");
    console.log("");

    if (tokens.refresh_token) {
      console.log("GOOGLE_REFRESH_TOKEN=");
      console.log(tokens.refresh_token);
      console.log("");
      console.log("Set this value in Render and in your local .env when needed.");
      console.log("Do not commit it.");
    } else {
      console.log("No refresh_token was returned.");
      console.log("Revoke the app grant in your Google account and run this script again.");
    }

    if (tokens.scope) {
      console.log("");
      console.log("Granted scopes:");
      console.log(tokens.scope);
    }

    setTimeout(() => {
      server.close();
      process.exit(0);
    }, 500);
  } catch (error) {
    res.writeHead(500, { "Content-Type": "text/html; charset=utf-8" });
    res.end(`<h1>Token exchange failed</h1><pre>${String(error.message || error)}</pre>`);
    console.error("Token exchange failed:", error.message || error);
  }
});

server.listen(port, () => {
  console.log(`Callback server listening on ${redirectURI}`);
});
