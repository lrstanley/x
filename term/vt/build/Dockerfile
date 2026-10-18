# Zig 0.16 toolchain used to compile lib-vt to wasm32-freestanding.
# The compiler stays in this image; the host only needs Docker and Go.
# The module is built into the image. docker run writes it to stdout.
FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl xz-utils git \
	&& rm -rf /var/lib/apt/lists/*

ARG ZIG_VERSION=0.16.0
ARG ZIG_SHA256=70e49664a74374b48b51e6f3fdfbf437f6395d42509050588bd49abe52ba3d00

RUN curl -fsSL -o /tmp/zig.tar.xz \
		"https://ziglang.org/download/${ZIG_VERSION}/zig-x86_64-linux-${ZIG_VERSION}.tar.xz" \
	&& echo "${ZIG_SHA256}  /tmp/zig.tar.xz" | sha256sum -c - \
	&& tar -C /opt -xJf /tmp/zig.tar.xz \
	&& ln -s "/opt/zig-x86_64-linux-${ZIG_VERSION}/zig" /usr/local/bin/zig \
	&& rm /tmp/zig.tar.xz

# Pinned Ghostty commit. generic keeps simd128 off so the translator does not
# require GOEXPERIMENT=simd.
ARG GHOSTTY_REV=e3ebb1d1bf772cf5d1bc70b5ae19e6943e33a110

RUN mkdir -p /src \
	&& cd /src \
	&& git init -q \
	&& git remote add origin https://github.com/ghostty-org/ghostty.git \
	&& git fetch --depth 1 origin "${GHOSTTY_REV}" \
	&& git checkout --detach --quiet FETCH_HEAD \
	&& zig build -Demit-lib-vt -Dtarget=wasm32-freestanding -Dcpu=generic -Doptimize=ReleaseSmall \
	&& mv zig-out/bin/ghostty-vt.wasm /ghostty-vt.wasm \
	&& rm -rf /src

CMD ["cat", "/ghostty-vt.wasm"]
