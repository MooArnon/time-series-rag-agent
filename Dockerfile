# Best Practice Python Dockerfile (Multi-stage build)

# Stage 1: Build Stage (Handles installation of dependencies)
## Use a specific, slim, and secure base image for the build environment
FROM python:3.12-slim-trixie AS build

# Set environment variables for better Python and pip behavior
ENV PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=off \
    PIP_DISABLE_PIP_VERSION_CHECK=on

# Create a non-root user and group for security (Best Practice)
RUN groupadd -r time-series-rag-agent && useradd -r -g time-series-rag-agent time-series-rag-agent

# Install dependencies (Copy only requirements.txt first for better caching)
COPY requirements.txt .

# Install dependencies. Use --default-timeout to handle slower mirrors if necessary.
RUN pip install --default-timeout=100 -r requirements.txt

# Set the working directory
WORKDIR /app

# Stage 2: Production Stage (Minimal image for running the application)
## Use a minimal base image (e.g., Python slim) to reduce final image size and attack surface
FROM python:3.12-slim-trixie AS final

# Recreate the non-root user and set permissions
RUN groupadd -r time-series-rag-agent && useradd -r -g time-series-rag-agent time-series-rag-agent \
    && mkdir /app \
    && chown -R time-series-rag-agent:time-series-rag-agent /app

WORKDIR /app

# Copy the installed dependencies from the build stage (Leveraging the multi-stage build)
COPY --from=build /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages

# Copy the application source code
COPY . /app

# Ensure non-root user ownership of copied files (important for security)
RUN chown -R time-series-rag-agent:time-series-rag-agent /app

# Set Matplotlib config directory to /tmp (writable by non-root)
ENV MPLCONFIGDIR=/tmp

# (Optional) If you are generating plots without a display (headless), ensure this is set:
ENV MPLBACKEND=Agg

# Set Numba cache directory to /tmp (writable by non-root)
ENV NUMBA_CACHE_DIR=/tmp
ENV NUMBA_DISABLE_JIT=1

# Switch to the non-root user for execution
USER time-series-rag-agent
