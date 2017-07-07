FROM busybox:glibc
COPY dyndns /dyndns
EXPOSE 53/udp
ENTRYPOINT ["/dyndns"]
