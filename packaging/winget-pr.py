#!/usr/bin/env python3
"""Sends the winget manifests of one version to microsoft/winget-pkgs.

winget-pr.py <version> <folder with the three manifests>

Needs GH_TOKEN, a token of the account that owns the fork
trelowney/winget-pkgs. Nothing is cloned: the commit
is made with the GitHub API on top of the newest winget-pkgs.

If a pull request for ttrace is still open (not yet accepted), that one is
switched to this version instead of opening a second one, which the winget
moderators do not want.
"""
import json
import os
import sys
import urllib.error
import urllib.request

UP = "microsoft/winget-pkgs"
FORK = "trelowney/winget-pkgs"
ID = "trelowney.ttrace"
DIR = "manifests/t/trelowney/ttrace"
NAME, EMAIL = "trelowney", "54177826+trelowney@users.noreply.github.com"


def gh(*args, data=None, ok404=False):
    """Calls the GitHub API like "gh api": gh("-X", "POST", path, data=...)."""
    method, path = "GET", args[-1]
    if args[0] == "-X":
        method = args[1]
    req = urllib.request.Request("https://api.github.com/" + path, method=method,
                                 data=json.dumps(data).encode() if data is not None else None,
                                 headers={"Authorization": "Bearer " + os.environ["GH_TOKEN"],
                                          "Accept": "application/vnd.github+json",
                                          "User-Agent": "ttrace-release"})
    try:
        with urllib.request.urlopen(req) as r:
            text = r.read().decode()
    except urllib.error.HTTPError as e:
        if ok404 and e.code == 404:
            return None
        sys.exit(f"{method} {path} failed: {e.code} {e.read().decode()[:500]}")
    return json.loads(text) if text.strip() else {}


def main():
    ver, folder = sys.argv[1], sys.argv[2]
    files = sorted(f for f in os.listdir(folder) if f.endswith(".yaml"))
    if len(files) != 3:
        sys.exit(f"expected 3 manifests in {folder}, found {files}")

    # newest winget-pkgs, also into the fork so it has every object
    gh("-X", "POST", f"repos/{FORK}/merge-upstream", data={"branch": "master"})
    base = gh(f"repos/{UP}/git/ref/heads/master")["object"]["sha"]
    base_tree = gh(f"repos/{UP}/git/commits/{base}")["tree"]["sha"]
    known = gh(f"repos/{UP}/contents/{DIR}", ok404=True) is not None

    tree = gh("-X", "POST", f"repos/{FORK}/git/trees", data={
        "base_tree": base_tree,
        "tree": [{"path": f"{DIR}/{ver}/{f}", "mode": "100644", "type": "blob",
                  "content": open(os.path.join(folder, f), encoding="utf-8").read()} for f in files],
    })["sha"]
    kind = "New version" if known else "New package"
    title = f"{kind}: {ID} version {ver}"
    commit = gh("-X", "POST", f"repos/{FORK}/git/commits", data={
        "message": title, "tree": tree, "parents": [base],
        "author": {"name": NAME, "email": EMAIL},
    })["sha"]

    # an open pull request of ours for ttrace?
    prs = gh(f"search/issues?q=repo:{UP}+is:pr+is:open+author:{NAME}+in:title+{ID}")["items"]
    prs = [p for p in prs if ID + " version" in p["title"]]
    body = (f"{kind}: ttrace {ver}, a network route tracer (like mtr / tracert) that shows the AS number, "
            f"AS name and country for every hop. Portable single executable, x64 and arm64, from the GitHub "
            f"release v{ver}. The manifests are made by the release build of ttrace, which also runs "
            f"`winget validate` and `winget install --manifest` on Windows before sending them.\n\n"
            "- [x] Have you signed the [Contributor License Agreement](https://cla.opensource.microsoft.com/microsoft/winget-pkgs)?\n"
            "- [x] Have you checked that there aren't other open pull requests for the same manifest update/change?\n"
            "- [x] This PR only modifies one (1) manifest\n"
            "- [x] Have you validated your manifest locally with `winget validate --manifest <path>`?\n"
            "- [x] Have you tested your manifest locally with `winget install --manifest <path>`?\n"
            "- [x] Does your manifest conform to the 1.12 schema?\n")

    if prs:
        num = prs[0]["number"]
        branch = gh(f"repos/{UP}/pulls/{num}")["head"]["ref"]
        gh("-X", "PATCH", f"repos/{FORK}/git/refs/heads/{branch}", data={"sha": commit, "force": True})
        gh("-X", "PATCH", f"repos/{UP}/pulls/{num}", data={"title": title, "body": body})
        print(f"switched the open pull request #{num} to {ver}")
        return

    branch = f"{ID}-{ver}"
    if gh(f"repos/{FORK}/git/ref/heads/{branch}", ok404=True) is None:
        gh("-X", "POST", f"repos/{FORK}/git/refs", data={"ref": f"refs/heads/{branch}", "sha": commit})
    else:
        gh("-X", "PATCH", f"repos/{FORK}/git/refs/heads/{branch}", data={"sha": commit, "force": True})
    pr = gh("-X", "POST", f"repos/{UP}/pulls", data={
        "title": title, "head": f"{NAME}:{branch}", "base": "master", "body": body,
    })
    print(f"opened pull request #{pr['number']}: {pr['html_url']}")


if __name__ == "__main__":
    main()
