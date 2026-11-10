# frozen_string_literal: true

require_relative 'test_helper'

class SseTest < Minitest::Test
  def parse(text, split: nil)
    parser = ToggleSlash::Sse::Parser.new
    chunks = split ? text.b.chars.each_slice(split).map(&:join) : [text]
    chunks.flat_map { |chunk| parser.feed(chunk) }.map { |event| [event.name, event.data] }
  end

  def test_names_and_data_in_every_split
    text = "event: changed\ndata: \"v2\"\n\n: comment\ndata: a\ndata: b\n\nevent: z\r\ndata: x\r\n\r\n"
    expected = [['changed', '"v2"'], ['message', "a\nb"], ['z', 'x']]
    [nil, 1, 2, 3, 7].each { |split| assert_equal expected, parse(text, split: split), "split #{split}" }
  end

  def test_a_lone_cr_ends_a_line_and_a_trailing_cr_waits_for_its_lf
    assert_equal [%w[message x]], parse("data: x\r\rdata: y")
    parser = ToggleSlash::Sse::Parser.new
    assert_empty parser.feed("data: x\r")
    assert_equal([%w[message x]], parser.feed("\n\n").map { |event| [event.name, event.data] })
  end

  def test_a_byte_order_mark_is_skipped_once_and_an_event_without_data_is_dropped
    assert_equal [%w[a 1]], parse("\xEF\xBB\xBFevent: a\ndata: 1\n\nevent: b\n\n".b)
  end

  def test_an_unfinished_event_is_never_returned_and_a_field_without_a_colon_has_an_empty_value
    assert_empty parse("event: a\ndata: 1\n")
    assert_equal [['message', '']], parse("data\n\n")
  end

  def test_invalid_utf8_is_scrubbed_not_raised
    events = parse("data: \xFF\n\n".b)
    assert_equal 1, events.size
    assert_predicate events.first[1], :valid_encoding?
  end
end
