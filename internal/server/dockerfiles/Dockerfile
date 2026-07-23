# Minimal runtime for Arma 3 dedicated server binaries mounted from the host.
# Game files are NOT baked in — Ideal bind-mounts DataRoot at the same absolute path.
FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		ca-certificates \
		lib32gcc-s1 \
		lib32stdc++6 \
		libatomic1 \
		libavahi-client3 \
		libstdc++6 \
		locales \
		net-tools \
	&& sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen \
	&& locale-gen \
	&& rm -rf /var/lib/apt/lists/*

ENV LANG=en_US.UTF-8 \
	LC_ALL=en_US.UTF-8

# Ideal supplies the working directory and command via docker run.
WORKDIR /
