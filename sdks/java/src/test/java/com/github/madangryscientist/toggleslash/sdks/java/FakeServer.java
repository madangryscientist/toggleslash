package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * A server written on raw sockets so a test sees every byte of every request and decides every byte
 * of the answer: HTTP/1.1 with keep-alive, a streaming answer that ends when the connection does,
 * an answer that never comes, and a connection that goes quiet.
 */
final class FakeServer implements AutoCloseable {
  /** One header as received. */
  record Header(String name, String value) {}

  /** One request as received. */
  record Request(String method, String path, List<Header> headers, byte[] body) {
    String header(String name) {
      for (Header h : headers) {
        if (h.name().equalsIgnoreCase(name)) {
          return h.value();
        }
      }
      return null;
    }

    String text() {
      return new String(body, StandardCharsets.UTF_8);
    }
  }

  /** What to do with a request. */
  interface Handler {
    void handle(Request request, Conn conn) throws Exception;
  }

  /** The connection a request came in on. */
  final class Conn {
    private final Socket socket;
    private final OutputStream out;
    private boolean streaming;

    private Conn(Socket socket) throws IOException {
      this.socket = socket;
      this.out = socket.getOutputStream();
    }

    /** Answers with a body and keeps the connection. */
    void respond(int status, String body, String... headers) throws IOException {
      byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
      StringBuilder head = new StringBuilder("HTTP/1.1 " + status + " " + reason(status) + "\r\n");
      for (int i = 0; i + 1 < headers.length; i += 2) {
        head.append(headers[i]).append(": ").append(headers[i + 1]).append("\r\n");
      }
      head.append("Content-Length: ").append(bytes.length).append("\r\n\r\n");
      write(head.toString().getBytes(StandardCharsets.ISO_8859_1));
      write(bytes);
    }

    /** Starts an answer with no length: the body is whatever {@link #write} sends until the end. */
    void begin(int status, String... headers) throws IOException {
      streaming = true;
      StringBuilder head = new StringBuilder("HTTP/1.1 " + status + " " + reason(status) + "\r\n");
      for (int i = 0; i + 1 < headers.length; i += 2) {
        head.append(headers[i]).append(": ").append(headers[i + 1]).append("\r\n");
      }
      head.append("Connection: close\r\n\r\n");
      write(head.toString().getBytes(StandardCharsets.ISO_8859_1));
    }

    void write(String text) throws IOException {
      write(text.getBytes(StandardCharsets.UTF_8));
    }

    synchronized void write(byte[] bytes) throws IOException {
      out.write(bytes);
      out.flush();
    }

    /** Waits until the client closes the connection (or the server does). */
    void awaitClientGone() {
      try {
        InputStream in = socket.getInputStream();
        while (in.read() >= 0) {
          // A client that has sent its request sends nothing more; it only closes.
        }
      } catch (IOException e) {
        // The connection is gone, which is what this waits for.
      }
    }

    /** Closes the connection. */
    void drop() {
      try {
        socket.close();
      } catch (IOException ignored) {
        // Already gone.
      }
    }
  }

  private final ServerSocket server;
  private final List<Request> requests = new CopyOnWriteArrayList<>();
  private final Set<Socket> sockets = ConcurrentHashMap.newKeySet();
  private final String scheme;
  private volatile Handler handler = (request, conn) -> conn.respond(404, "");
  private volatile boolean closed;

  FakeServer() throws IOException {
    this(new ServerSocket(0, 50, InetAddress.getLoopbackAddress()), "http");
  }

  FakeServer(ServerSocket server, String scheme) {
    this.server = server;
    this.scheme = scheme;
    Thread accept = new Thread(this::acceptLoop, "fake-server-accept");
    accept.setDaemon(true);
    accept.start();
  }

  String url() {
    return scheme + "://127.0.0.1:" + server.getLocalPort();
  }

  void handler(Handler next) {
    this.handler = next;
  }

  List<Request> requests() {
    return new ArrayList<>(requests);
  }

  List<Request> requests(String path) {
    List<Request> matching = new ArrayList<>();
    for (Request r : requests) {
      if (r.path().equals(path)) {
        matching.add(r);
      }
    }
    return matching;
  }

  private void acceptLoop() {
    while (!closed) {
      try {
        Socket socket = server.accept();
        sockets.add(socket);
        Thread thread = new Thread(() -> serve(socket), "fake-server-conn");
        thread.setDaemon(true);
        thread.start();
      } catch (IOException e) {
        return;
      }
    }
  }

  private void serve(Socket socket) {
    try (socket) {
      InputStream in = socket.getInputStream();
      Conn conn = new Conn(socket);
      while (!closed) {
        Request request = read(in);
        if (request == null) {
          return;
        }
        requests.add(request);
        handler.handle(request, conn);
        if (conn.streaming) {
          return;
        }
      }
    } catch (Exception e) {
      // A closed socket ends the connection; the test sees what it needs in the requests.
    } finally {
      sockets.remove(socket);
    }
  }

  private static Request read(InputStream in) throws IOException {
    ByteArrayOutputStream head = new ByteArrayOutputStream();
    int matched = 0;
    while (matched < 4) {
      int b = in.read();
      if (b < 0) {
        return null;
      }
      head.write(b);
      boolean expected = b == (matched % 2 == 0 ? '\r' : '\n');
      matched = expected ? matched + 1 : (b == '\r' ? 1 : 0);
    }
    String[] lines = head.toString(StandardCharsets.ISO_8859_1).split("\r\n");
    String[] start = lines[0].split(" ");
    List<Header> headers = new ArrayList<>();
    int length = 0;
    for (int i = 1; i < lines.length; i++) {
      int colon = lines[i].indexOf(':');
      if (colon <= 0) {
        continue;
      }
      String name = lines[i].substring(0, colon);
      String value = lines[i].substring(colon + 1).trim();
      headers.add(new Header(name, value));
      if (name.toLowerCase(Locale.ROOT).equals("content-length")) {
        length = Integer.parseInt(value);
      }
    }
    byte[] body = in.readNBytes(length);
    return new Request(start[0], start[1], headers, body);
  }

  private static String reason(int status) {
    return switch (status) {
      case 200 -> "OK";
      case 304 -> "Not Modified";
      case 400 -> "Bad Request";
      case 429 -> "Too Many Requests";
      case 500 -> "Internal Server Error";
      case 503 -> "Service Unavailable";
      default -> "Status";
    };
  }

  @Override
  public void close() {
    closed = true;
    try {
      server.close();
    } catch (IOException ignored) {
      // Closing.
    }
    for (Socket socket : sockets) {
      try {
        socket.close();
      } catch (IOException ignored) {
        // Closing.
      }
    }
  }
}
