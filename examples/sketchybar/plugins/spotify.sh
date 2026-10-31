#!/usr/bin/env bash
# Copy to ~/.config/sketchybar/plugins/spotify.sh and run: chmod +x spotify.sh
#
# SketchyBar becomes the responsible process for the macOS Automation prompt.
# Allowing your terminal does not cover SketchyBar: when the plugin first runs,
# allow SketchyBar when macOS asks.

label="$(spmeta --template '{artist} – {title}' 2>/dev/null)"
sketchybar --set "$NAME" label="$label" drawing="$([ -n "$label" ] && echo on || echo off)"
