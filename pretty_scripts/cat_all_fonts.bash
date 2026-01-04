#!/bin/bash

figlet_fonts=$(find /opt/homebrew/Cellar/figlet/2.2.5/share/figlet/fonts -name "*.flf" -type f | sed 's|.*/||' | sed 's|\.flf||' | sort)

for font in $figlet_fonts; do
    echo "--------------------------------"
    echo "Using font: $font"
    echo "--------------------------------"
    echo "FIGLET OUTPUT:"
    figlet -f "$font" "Orla"
    echo "--------------------------------"
done

toilet_fonts=$(find /opt/homebrew/Cellar/toilet/0.3/share/figlet -name "*.tlf" -type f | sed 's|.*/||' | sed 's|\.tlf||' | sort)

for font in $toilet_fonts; do
    echo "--------------------------------"
    echo "Using font: $font"
    echo "--------------------------------"
    echo "TOILET OUTPUT:"
    toilet -f "$font" "Orla"
    echo "--------------------------------"
done
