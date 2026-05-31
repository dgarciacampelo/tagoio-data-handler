# * Stage 1: Tailwind CSS Builder
FROM python:3.12-slim AS css-builder

# Install curl to download the standalone CLI
RUN apt-get update && apt-get install -y curl

# Download the Tailwind Standalone CLI for Linux x64
RUN curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64 \
    && chmod +x tailwindcss-linux-x64 \
    && mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss

WORKDIR /build

# Copy only the files Tailwind needs to scan (config, HTML, JS)
COPY tailwind.config.js .
COPY templates ./templates
COPY static ./static

# Compile and minify the CSS
RUN tailwindcss -i ./static/css/input.css -o ./static/css/output.css --minify

# * Stage 2: Final Application Image
FROM python:3.12-slim

# Creation of src directory
WORKDIR /src

# Copy Python libraries requirements and install them
COPY requirements.txt .
RUN python3.12 -m pip install -r requirements.txt

# Copy source code to working directory
COPY src .
COPY templates ./templates
COPY static ./static

# Inject the compiled CSS from Stage 1 (Overwriting the uncompiled version)
COPY --from=css-builder /build/static/css/output.css ./static/css/output.css

# Launch the Python main.py
CMD ["python3.12", "main.py"]