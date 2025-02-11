#!/usr/bin/env bash

input_image="$1"
out_dir="$2"

# Favicon (32x32 or 48x48)
magick "${input_image}" -resize 64x64 "${out_dir}/favicon.png"

# Open Graph image (1200x630)
magick "${input_image}" -resize 1200x630 "${out_dir}/og-image.png"

# Twitter card image (1024x512)
magick "${input_image}" -resize 1024x512 "${out_dir}/twitter-image.png"
