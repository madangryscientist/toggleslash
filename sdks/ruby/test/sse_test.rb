# frozen_string_literal: true

require_relative "test_helper"

class SseTest < Minitest::Test
  def parse(*chunks)
    parser = Toggleslash::SseParser.new
    chunks.flat_map { |chunk| parser.feed(chunk.b) }
  end

  def test_named_events_and_the_default_name
    assert_equal [%w[changed x], %w[message y]], parse("event: changed\ndata: x\n\ndata: y\n\n")
  end

  def test_line_ends_lf_crlf_and_lone_cr
    assert_equal [%w[a 1], %w[b 2], %w[c 3]],
                 parse("event: a\ndata: 1\n\nevent: b\r\ndata: 2\r\n\r\nevent: c\rdata: 3\r\r")
  end

  def test_crlf_split_across_reads
    assert_equal [%w[a 1]], parse("event: a\r", "\ndata: 1\r", "\n\r", "\n")
  end

  def test_several_data_lines_join_with_lf
    assert_equal [%W[message a\nb]], parse("data: a\ndata: b\n\n")
  end

  def test_comments_unknown_fields_and_an_event_with_no_data_are_ignored
    assert_equal [], parse(": keep alive\nid: 7\nretry: 5\nfoo: bar\n\nevent: only\n\n")
  end

  def test_utf8_split_across_reads
    bytes = "data: \u00e9\u4e2d\n\n".b
    assert_equal [%w[message é中]], parse(bytes.byteslice(0, 8), bytes.byteslice(8, bytes.bytesize))
  end

  def test_a_byte_order_mark_at_the_start_is_skipped_even_when_split
    assert_equal [%w[message x]], parse("\xEF\xBB\xBFdata: x\n\n".b)
    assert_equal [%w[message x]], parse("\xEF".b, "\xBB\xBFdata: x\n\n".b)
  end

  def test_an_incomplete_event_is_never_returned
    assert_equal [], parse("event: a\ndata: 1\n")
  end

  def test_invalid_utf8_becomes_the_replacement_character
    assert_equal [["message", "a\uFFFDb"]], parse("data: a\xFFb\n\n")
  end

  def test_a_value_may_omit_the_space_and_keeps_a_second_one
    assert_equal [["message", " x"], ["message", "y"]], parse("data:  x\n\ndata:y\n\n")
  end
end
