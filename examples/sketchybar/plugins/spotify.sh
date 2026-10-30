#!/usr/bin/env bash
# Copy to ~/.config/sketchybar/plugins/spotify.sh and run: chmod +x spotify.sh
#
# SketchyBar becomes the responsible process for the macOS Automation prompt.
# Run `spmeta doctor` once from a terminal and allow SketchyBar when macOS asks.

label="$(spmeta --template '{artist} – {title}' 2>/dev/null)"
sketchybar --set "$NAME" label="$label" drawing="$([ -n "$label" ] && echo on || echo off)"
