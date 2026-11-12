package com.github.madangryscientist.toggleslash.sdks.java;

/** A daemon thread the SDK starts, so the SDK can tell its own threads from a caller's. */
final class SdkThread extends Thread {
  SdkThread(Runnable body, String name) {
    super(body, name);
    setDaemon(true);
  }
}
