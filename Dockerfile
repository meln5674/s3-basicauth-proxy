FROM scratch

COPY bin/proxy /proxy

EXPOSE 8080

ENTRYPOINT [ "/proxy" ]
CMD [ "-listen-addr", "0.0.0.0", "-listen-port", "8080" ]

