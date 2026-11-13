import XCTest
@testable import Mewla

/// The live surfaces' rules (Shared/MewlaLiveView.swift), as Android's LiveViewTest checks them.
final class LiveViewTests: XCTestCase {
  private let wire = """
    {"kind":"needs_you","epoch":3,"seq":9,"run_key":"ask:q1","title":"Pricing page","needs_count":2,
     "question":{"id":"q1","kind":"work","work_id":"w-price","text":"14-day or 7-day trial?","choices":["14-day","7-day"]},
     "running":2,"waiting":1,
     "items":[
      {"key":"work:w-price","open":"work","id":"w-price","title":"Pricing page","line":"14-day or 7-day trial?","agent":"claude","needs":"answer"},
      {"key":"session:s9","open":"session","id":"s9","title":"infra","line":"Run git push origin main","agent":"codex","needs":"approve"},
      {"key":"work:w1","open":"session","id":"s1","title":"Quickstart rewrite","line":"Editing docs","agent":"claude","ring":{"stage":"moving","percent":45},"started_at":1700000000000}
     ]}
    """

  private func parse(_ raw: String) throws -> MewlaLiveState { try XCTUnwrap(MewlaLiveState.parse(raw)) }

  func testParsesItemsRingsAndCounts() throws {
    let state = try parse(wire)
    XCTAssertEqual(state.items.count, 3)
    XCTAssertEqual(state.running, 2)
    XCTAssertEqual(state.waiting, 1)
    XCTAssertEqual(try XCTUnwrap(state.items[2].ring).fraction, 0.45, accuracy: 0.001)
    XCTAssertTrue(state.items[0].asks)
    XCTAssertNil(state.items[0].ring)
    XCTAssertEqual(state.question?.workId, "w-price")
  }

  func testAnUnknownRingStageDrawsNoRing() throws {
    let state = try parse(#"{"kind":"running","items":[{"key":"k","open":"work","title":"t","ring":{"stage":"unmeasured"}}]}"#)
    XCTAssertNil(state.items[0].ring)
  }

  func testTheStateRoundTripsAsTheActivityContent() throws {
    let state = try parse(wire)
    let again = try JSONDecoder().decode(MewlaLiveState.self, from: JSONEncoder().encode(state))
    XCTAssertEqual(again, state)
  }

  func testFocusIsTheNewestRequestAndTheHeaderSaysTheCount() throws {
    let state = try parse(wire)
    XCTAssertEqual(MewlaLiveView.focus(state)?.key, "work:w-price")
    XCTAssertEqual(MewlaLiveView.headerWords(state), "2 need you")
    XCTAssertEqual(MewlaLiveView.tag(state.items[0]), "Answer")
    XCTAssertEqual(MewlaLiveView.tag(state.items[1]), "Approve")
  }

  func testChoicesAnswerOnlyTheWorkQuestion() throws {
    let state = try parse(wire)
    XCTAssertEqual(MewlaLiveView.choices(state, state.items[0]), ["14-day", "7-day"])
    XCTAssertEqual(MewlaLiveView.choices(state, state.items[1]), [])
    XCTAssertEqual(MewlaLiveView.choices(state, state.items[2]), [])
  }

  func testLinksOpenTheItem() throws {
    let state = try parse(wire)
    XCTAssertEqual(MewlaLiveView.link(state.items[0]).absoluteString, "mewla://brain/work/w-price")
    XCTAssertEqual(MewlaLiveView.link(state.items[2]).absoluteString, "mewla://sessions/s1")
    XCTAssertEqual(MewlaLiveView.link(nil).absoluteString, "mewla://brain")
  }

  func testRunningAndQuietWords() throws {
    let running = try parse(#"{"kind":"running","running":3,"items":[{"key":"a","open":"session","id":"a","title":"A"}]}"#)
    XCTAssertEqual(MewlaLiveView.headerWords(running), "3 running")
    XCTAssertEqual(MewlaLiveView.othersRunning(running, running.items[0]), "+2 running")
    XCTAssertEqual(MewlaLiveView.headerWords(try parse(#"{"kind":"idle","queued":1,"waiting":2}"#)), "1 queued, 2 waiting")
    XCTAssertEqual(MewlaLiveView.headerWords(try parse(#"{"kind":"idle"}"#)), "All quiet")
  }

  func testAnOlderDaemonStillShowsItsQuestionOrItsRun() throws {
    let asking = try parse(#"{"kind":"needs_you","needs_count":1,"title":"Ship","question":{"id":"q","work_id":"w","text":"Ship it?","choices":["Ship"]}}"#)
    let item = try XCTUnwrap(MewlaLiveView.focus(asking))
    XCTAssertEqual(item.title, "Ship")
    XCTAssertEqual(MewlaLiveView.choices(asking, item), ["Ship"])
    let running = try parse(#"{"kind":"running","title":"perpetuo","phase":"releasing","started_at":5}"#)
    XCTAssertEqual(MewlaLiveView.focus(running)?.title, "perpetuo")
    XCTAssertEqual(MewlaLiveView.headerWords(running), "1 running")
    XCTAssertNil(MewlaLiveView.focus(try parse(#"{"kind":"idle"}"#)))
  }
}
