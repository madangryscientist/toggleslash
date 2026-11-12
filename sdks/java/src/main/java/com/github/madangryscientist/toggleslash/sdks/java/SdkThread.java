package com.github.madangryscientist.toggleslash.sdks.java;

/** A daemon thread that belongs to one client, so a callback can tell it is on the SDK's own. */
final class SdkThread extends Thread {
  private final Object owner;

  SdkThread(Object owner, String name, Runnable body) {
    super(body, name);
    this.owner = owner;
    setDaemon(true);
  }

  /** True when the calling thread is one of {@code owner}'s own. */
  static boolean isOwnedBy(Object owner) {
    Thread current = Thread.currentThread();
    return current instanceof SdkThread && ((SdkThread) current).owner == owner;
  }
}
