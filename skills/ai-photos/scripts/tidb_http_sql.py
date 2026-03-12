#!/usr/bin/env python3
import argparse
import base64
import json
import urllib.request


def derive_http_host(db_host: str) -> str:
    db_host = db_host.strip()
    if db_host.startswith("http://") or db_host.startswith("https://"):
        return db_host.rstrip("/")
    return f"https://http-{db_host}/v1beta/sql"


def run_query(host, username, password, database, query):
    token = base64.b64encode(f"{username}:{password}".encode()).decode()
    req = urllib.request.Request(
        derive_http_host(host),
        data=json.dumps({"query": query}).encode(),
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Basic {token}",
            "TiDB-Database": database,
            "User-Agent": "ai-photos/0.1",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read().decode()


def load_target(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def main():
    ap = argparse.ArgumentParser(description="Run SQL against TiDB HTTP SQL API")
    ap.add_argument("target", help="path to TiDB HTTP target JSON")
    ap.add_argument("--query", required=True)
    args = ap.parse_args()
    target = load_target(args.target)
    print(run_query(target["host"], target["username"], target["password"], target["database"], args.query))

if __name__ == "__main__":
    main()
