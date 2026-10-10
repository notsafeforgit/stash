#!/usr/bin/env python3
"""Partition the complete tagged Go package list without maintaining allowlists."""

import argparse
import subprocess


def select_packages(packages, module, suite):
    root = module + "/pkg/sqlite"
    selected = [
        package for package in packages
        if (package == root or package.startswith(root + "/")) == (suite == "sqlite")
    ]
    if not selected:
        raise ValueError(f"No Go packages selected for {suite}")
    return selected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("suite", choices=("sqlite", "other"))
    args = parser.parse_args()
    module = subprocess.check_output(["go", "list", "-m"], text=True).strip()
    packages = subprocess.check_output(["make", "--no-print-directory", "-s", "list-test-packages"], text=True).split()
    print(" ".join(select_packages(packages, module, args.suite)))


if __name__ == "__main__":
    main()
